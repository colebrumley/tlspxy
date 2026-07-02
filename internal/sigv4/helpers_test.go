package sigv4

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
)

// signInbound builds and SigV4-signs a request the way a genuine client would,
// using the real aws-sdk-go-v2 v4 signer so verification is exercised against
// real signatures.
func signInbound(t *testing.T, method, rawurl string, body []byte, akid, secret, service, region string, when time.Time, payloadHash string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, rawurl, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.ContentLength = int64(len(body))
	// Real AWS clients present the payload hash in X-Amz-Content-Sha256 and
	// include it in the signed headers; mirror that so the signature covers it.
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	signer := v4.NewSigner()
	creds := aws.Credentials{AccessKeyID: akid, SecretAccessKey: secret}
	if err := signer.SignHTTP(context.Background(), creds, req, payloadHash, service, region, when); err != nil {
		t.Fatalf("SignHTTP: %v", err)
	}
	// SignHTTP does not consume the body, but ensure a fresh reader for the
	// handler to read.
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.7:4444"
	return req
}

// writeKeystore writes a keystore YAML file into a temp dir and returns its path.
func writeKeystore(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.yml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// fakeTransport records outbound requests and returns a canned response.
type fakeTransport struct {
	mu      sync.Mutex
	called  int
	lastReq *http.Request
	status  int
	extra   http.Header
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.called++
	f.lastReq = r.Clone(r.Context())
	status := f.status
	f.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	hdr := make(http.Header)
	hdr.Set("Content-Type", "application/xml")
	for k, vv := range f.extra {
		hdr[k] = vv
	}
	return &http.Response{
		StatusCode: status,
		Header:     hdr,
		Body:       io.NopCloser(bytes.NewReader([]byte("<ok/>"))),
	}, nil
}

func (f *fakeTransport) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}

func (f *fakeTransport) outboundAuthorization() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastReq == nil {
		return ""
	}
	return f.lastReq.Header.Get("Authorization")
}

// fakeSTS is a stand-in for the STS AssumeRole API. It records which roles were
// assumed and returns per-role distinct temporary credentials.
type fakeSTS struct {
	mu        sync.Mutex
	callCount map[string]int
	expiry    time.Time
	credFor   func(roleARN string) (accessKeyID, secret string)
}

func newFakeSTS(expiry time.Time) *fakeSTS {
	return &fakeSTS{
		callCount: make(map[string]int),
		expiry:    expiry,
		credFor: func(roleARN string) (string, string) {
			// Derive a deterministic, distinct access key id per role.
			return "ASIA" + shortHash(roleARN), "secret-" + shortHash(roleARN)
		},
	}
}

func (f *fakeSTS) AssumeRole(ctx context.Context, in *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	role := aws.ToString(in.RoleArn)
	f.callCount[role]++
	ak, sk := f.credFor(role)
	exp := f.expiry
	tok := "token-" + shortHash(role)
	return &sts.AssumeRoleOutput{
		Credentials: &ststypes.Credentials{
			AccessKeyId:     aws.String(ak),
			SecretAccessKey: aws.String(sk),
			SessionToken:    aws.String(tok),
			Expiration:      aws.Time(exp),
		},
	}, nil
}

func (f *fakeSTS) count(role string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount[role]
}

func shortHash(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	const hexdig = "0123456789ABCDEF"
	out := make([]byte, 8)
	for i := 0; i < 8; i++ {
		out[7-i] = hexdig[h&0xf]
		h >>= 4
	}
	return string(out)
}

// staticBase returns a static base credentials provider for tests.
func staticBase() aws.CredentialsProvider {
	return credentials.NewStaticCredentialsProvider("AKIABASE", "basesecret", "")
}
