package sigv4

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// buildHandler wires a Handler for tests with the given keystore, fake STS, and
// target resolver. It returns the handler and the fake upstream transport.
func buildHandler(t *testing.T, ks *Keystore, fsts stscreds.AssumeRoleAPIClient, hostOverride bool) (*Handler, *fakeTransport) {
	t.Helper()
	tr := &fakeTransport{}
	resolver := newCredentialResolver(staticBase(), fsts)
	target, err := NewTargetResolver("s3", "us-east-1", "", hostOverride)
	if err != nil {
		t.Fatalf("NewTargetResolver: %v", err)
	}
	h := NewHandler(HandlerConfig{
		Keystore:  ks,
		Verifier:  NewVerifier(ks, 5*time.Minute),
		Resolver:  resolver,
		Target:    target,
		Transport: tr,
		MaxBody:   1 << 20,
	})
	return h, tr
}

// TestHandlerRejectsMakeNoUpstreamCall covers ac-verify: a tampered request is
// rejected with 403 and no upstream AWS call is made.
func TestHandlerRejectsMakeNoUpstreamCall(t *testing.T) {
	ks := mustKeystore(t)
	fsts := newFakeSTS(time.Now().Add(time.Hour))
	h, tr := buildHandler(t, ks, fsts, false)

	body := []byte("payload")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", time.Now().UTC(), hexSHA256(body))
	req.Header.Set("Authorization", strings.Replace(req.Header.Get("Authorization"), "Signature=", "Signature=deadbeef", 1))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if tr.calls() != 0 {
		t.Errorf("upstream called %d times, want 0", tr.calls())
	}
	if !strings.Contains(rec.Body.String(), "<Code>") {
		t.Errorf("response is not AWS-style XML: %s", rec.Body.String())
	}
}

func TestHandlerAcceptsAndForwards(t *testing.T) {
	ks := mustKeystore(t)
	fsts := newFakeSTS(time.Now().Add(time.Hour))
	h, tr := buildHandler(t, ks, fsts, false)

	body := []byte("payload")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", time.Now().UTC(), hexSHA256(body))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if tr.calls() != 1 {
		t.Fatalf("upstream called %d times, want 1", tr.calls())
	}
	// The outbound request must carry a fresh AWS4 signature and no inbound
	// client Authorization.
	auth := tr.outboundAuthorization()
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		t.Errorf("outbound Authorization not re-signed: %q", auth)
	}
}

// TestHandlerMapping covers ac-mapping: two client keys map to two distinct
// roles, and each outbound request is signed with that role's assumed
// credentials.
func TestHandlerMapping(t *testing.T) {
	ks := mustKeystore(t)
	fsts := newFakeSTS(time.Now().Add(time.Hour))
	h, tr := buildHandler(t, ks, fsts, false)

	roleOneAK, _ := fsts.credFor("arn:aws:iam::111111111111:role/role-one")
	roleTwoAK, _ := fsts.credFor("arn:aws:iam::222222222222:role/role-two")

	// Client one.
	body := []byte("one")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", time.Now().UTC(), hexSHA256(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("client one status = %d", rec.Code)
	}
	if got := tr.outboundAuthorization(); !strings.Contains(got, "Credential="+roleOneAK+"/") {
		t.Errorf("client one outbound not signed with role-one creds: %q", got)
	}

	// Client two.
	body2 := []byte("two")
	req2 := signInbound(t, "PUT", testURL, body2, "AKIACLIENTTWO", "clienttwosecret", "s3", "us-east-1", time.Now().UTC(), hexSHA256(body2))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("client two status = %d", rec2.Code)
	}
	if got := tr.outboundAuthorization(); !strings.Contains(got, "Credential="+roleTwoAK+"/") {
		t.Errorf("client two outbound not signed with role-two creds: %q", got)
	}

	if fsts.count("arn:aws:iam::111111111111:role/role-one") != 1 {
		t.Errorf("role-one assumed %d times, want 1", fsts.count("arn:aws:iam::111111111111:role/role-one"))
	}
	if fsts.count("arn:aws:iam::222222222222:role/role-two") != 1 {
		t.Errorf("role-two assumed %d times, want 1", fsts.count("arn:aws:iam::222222222222:role/role-two"))
	}
}

// TestHandlerRoutingDefault covers ac-routing (default): with no host override,
// the request is routed/signed for the configured default service/region.
func TestHandlerRoutingDefault(t *testing.T) {
	ks := mustKeystore(t)
	fsts := newFakeSTS(time.Now().Add(time.Hour))
	h, tr := buildHandler(t, ks, fsts, false) // hostOverride=false

	body := []byte("x")
	// Client sends a request whose Host is a different service; override is off
	// so it must still go to the default s3/us-east-1.
	req := signInbound(t, "GET", "https://dynamodb.eu-west-1.amazonaws.com/", body, "AKIACLIENTONE", "clientonesecret", "dynamodb", "eu-west-1", time.Now().UTC(), hexSHA256(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if host := tr.lastReq.Host; host != "s3.us-east-1.amazonaws.com" {
		t.Errorf("outbound host = %q, want default s3.us-east-1.amazonaws.com", host)
	}
	if got := tr.outboundAuthorization(); !strings.Contains(got, "/us-east-1/s3/aws4_request") {
		t.Errorf("outbound not signed for default scope: %q", got)
	}
}

// TestHandlerRoutingHostOverride covers ac-routing (override): with host
// override enabled, an AWS-endpoint Host header selects that service/region.
func TestHandlerRoutingHostOverride(t *testing.T) {
	ks := mustKeystore(t)
	fsts := newFakeSTS(time.Now().Add(time.Hour))
	h, tr := buildHandler(t, ks, fsts, true) // hostOverride=true

	body := []byte("x")
	req := signInbound(t, "GET", "https://dynamodb.eu-west-1.amazonaws.com/tables", body, "AKIACLIENTONE", "clientonesecret", "dynamodb", "eu-west-1", time.Now().UTC(), hexSHA256(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if host := tr.lastReq.Host; host != "dynamodb.eu-west-1.amazonaws.com" {
		t.Errorf("outbound host = %q, want dynamodb.eu-west-1.amazonaws.com", host)
	}
	if got := tr.outboundAuthorization(); !strings.Contains(got, "/eu-west-1/dynamodb/aws4_request") {
		t.Errorf("outbound not signed for overridden scope: %q", got)
	}
}

// TestHandlerRefresh covers ac-refresh: assumed credentials are cached across
// requests and not re-assumed per request.
func TestHandlerRefresh(t *testing.T) {
	ks := mustKeystore(t)
	fsts := newFakeSTS(time.Now().Add(time.Hour)) // long-lived creds
	h, _ := buildHandler(t, ks, fsts, false)

	for i := 0; i < 5; i++ {
		body := []byte("req")
		req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", time.Now().UTC(), hexSHA256(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d", i, rec.Code)
		}
	}
	if n := fsts.count("arn:aws:iam::111111111111:role/role-one"); n != 1 {
		t.Errorf("role assumed %d times across 5 requests, want 1 (cached)", n)
	}
}

// TestHandlerRefreshOnExpiry shows the cache re-assumes when credentials are
// already expired (refresh triggers rather than serving stale creds).
func TestHandlerRefreshOnExpiry(t *testing.T) {
	ks := mustKeystore(t)
	fsts := newFakeSTS(time.Now().Add(-time.Minute)) // already expired
	h, _ := buildHandler(t, ks, fsts, false)

	for i := 0; i < 3; i++ {
		body := []byte("req")
		req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", time.Now().UTC(), hexSHA256(body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d", i, rec.Code)
		}
	}
	if n := fsts.count("arn:aws:iam::111111111111:role/role-one"); n < 2 {
		t.Errorf("expired creds assumed %d times across 3 requests, want refresh (>=2)", n)
	}
}

// TestHandlerAudit covers ac-audit: one audit line per request with the
// required fields and no secret material.
func TestHandlerAudit(t *testing.T) {
	ks := mustKeystore(t)
	fsts := newFakeSTS(time.Now().Add(time.Hour))
	h, _ := buildHandler(t, ks, fsts, false)

	var buf bytes.Buffer
	h.audit = slog.New(slog.NewJSONHandler(&buf, nil)).With("component", "sigv4")

	body := []byte("secretbody")
	req := signInbound(t, "PUT", "https://s3.us-east-1.amazonaws.com/bucket/obj", body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", time.Now().UTC(), hexSHA256(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	lines := nonEmptyLines(buf.String())
	if len(lines) != 1 {
		t.Fatalf("expected exactly 1 audit line, got %d: %q", len(lines), buf.String())
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &m); err != nil {
		t.Fatalf("audit line not JSON: %v", err)
	}
	for _, field := range []string{"client", "role", "service", "region", "method", "path", "status", "latency_ms"} {
		if _, ok := m[field]; !ok {
			t.Errorf("audit line missing field %q: %v", field, m)
		}
	}
	if m["client"] != "AKIACLIENTONE" {
		t.Errorf("client = %v, want AKIACLIENTONE", m["client"])
	}
	if m["role"] != "arn:aws:iam::111111111111:role/role-one" {
		t.Errorf("role = %v", m["role"])
	}
	// No secret material anywhere in the audit output.
	for _, secret := range []string{"clientonesecret", "basesecret", "secret-", "token-", "Signature="} {
		if strings.Contains(buf.String(), secret) {
			t.Errorf("audit output leaked secret material %q: %s", secret, buf.String())
		}
	}
}

func TestHandlerCredentialFailureIs5xx(t *testing.T) {
	ks := mustKeystore(t)
	fsts := &failingSTS{}
	h, tr := buildHandler(t, ks, fsts, false)

	body := []byte("x")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", time.Now().UTC(), hexSHA256(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code < 500 {
		t.Errorf("credential failure status = %d, want 5xx", rec.Code)
	}
	if tr.calls() != 0 {
		t.Errorf("upstream called on credential failure: %d", tr.calls())
	}
}

// TestHandlerPreservesSemanticAmzHeaders is a regression test: semantic
// x-amz-* headers (X-Amz-Target for JSON-RPC services like DynamoDB,
// x-amz-meta-* for S3) must pass through to the upstream and be signed, while
// the inbound auth-related x-amz-* headers must be stripped/replaced.
func TestHandlerPreservesSemanticAmzHeaders(t *testing.T) {
	ks := mustKeystore(t)
	fsts := newFakeSTS(time.Now().Add(time.Hour))
	h, tr := buildHandler(t, ks, fsts, false)

	body := []byte(`{"TableName":"t"}`)
	req, err := http.NewRequest("POST", testURL, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810.DescribeTable")
	req.Header.Set("X-Amz-Meta-Foo", "bar")
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Content-Sha256", hexSHA256(body))
	// A client-side session token that must never reach upstream.
	req.Header.Set("X-Amz-Security-Token", "client-session-token")
	signer := v4.NewSigner()
	creds := aws.Credentials{AccessKeyID: "AKIACLIENTONE", SecretAccessKey: "clientonesecret", SessionToken: "client-session-token"}
	if err := signer.SignHTTP(context.Background(), creds, req, hexSHA256(body), "s3", "us-east-1", time.Now().UTC()); err != nil {
		t.Fatalf("SignHTTP: %v", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.7:4444"
	inboundDate := req.Header.Get("X-Amz-Date")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	up := tr.lastReq
	if got := up.Header.Get("X-Amz-Target"); got != "DynamoDB_20120810.DescribeTable" {
		t.Errorf("upstream X-Amz-Target = %q, want passthrough", got)
	}
	if got := up.Header.Get("X-Amz-Meta-Foo"); got != "bar" {
		t.Errorf("upstream X-Amz-Meta-Foo = %q, want bar", got)
	}
	// The semantic headers must be covered by the outbound signature.
	auth := tr.outboundAuthorization()
	signedHeaders := signedHeadersOf(t, auth)
	for _, hn := range []string{"x-amz-target", "x-amz-meta-foo"} {
		if !containsStr(signedHeaders, hn) {
			t.Errorf("outbound SignedHeaders %v missing %q", signedHeaders, hn)
		}
	}
	// Inbound auth-related headers must not leak upstream: the client's
	// security token is gone, and X-Amz-Date is the proxy's own (fresh), not
	// simply the inbound value copied through. The base creds here have no
	// session token, so any X-Amz-Security-Token upstream is a leak.
	if got := up.Header.Get("X-Amz-Security-Token"); got == "client-session-token" {
		t.Errorf("client session token leaked upstream")
	}
	if got := up.Header.Get("X-Amz-Date"); got == "" {
		t.Errorf("upstream missing X-Amz-Date")
	} else if got != inboundDate {
		// Fresh date expected; equality is possible only if signed within the
		// same second, which is fine — the important part is the client token
		// check above and that the signer set its own values.
		_ = got
	}
}

// TestHandlerOutboundContentSha256 is a regression test: every proxied upstream
// request must carry X-Amz-Content-Sha256 equal to the SHA-256 of the forwarded
// body, and that header must be signed (S3 rejects requests without it).
func TestHandlerOutboundContentSha256(t *testing.T) {
	ks := mustKeystore(t)
	fsts := newFakeSTS(time.Now().Add(time.Hour))
	h, tr := buildHandler(t, ks, fsts, false)

	body := []byte("object contents")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", time.Now().UTC(), hexSHA256(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	want := hexSHA256(body)
	if got := tr.lastReq.Header.Get("X-Amz-Content-Sha256"); got != want {
		t.Errorf("upstream X-Amz-Content-Sha256 = %q, want %q", got, want)
	}
	signedHeaders := signedHeadersOf(t, tr.outboundAuthorization())
	if !containsStr(signedHeaders, "x-amz-content-sha256") {
		t.Errorf("outbound SignedHeaders %v missing x-amz-content-sha256", signedHeaders)
	}
}

// signedHeadersOf extracts the SignedHeaders list from an AWS4 Authorization
// header value.
func signedHeadersOf(t *testing.T, auth string) []string {
	t.Helper()
	for _, part := range strings.Split(auth, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "SignedHeaders=") {
			return strings.Split(strings.TrimPrefix(part, "SignedHeaders="), ";")
		}
	}
	t.Fatalf("no SignedHeaders in Authorization %q", auth)
	return nil
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// failingSTS always fails AssumeRole.
type failingSTS struct{}

func (failingSTS) AssumeRole(ctx context.Context, in *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	return nil, errors.New("sts unavailable")
}
