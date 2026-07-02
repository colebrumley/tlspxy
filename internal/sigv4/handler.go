package sigv4

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// Handler is the SigV4 credential-translation gateway. It verifies inbound
// client signatures, re-signs verified requests with outbound AWS credentials
// mapped from the client identity, forwards them to the AWS endpoint, and emits
// one audit log line per request.
//
// It implements http.Handler and replaces the plain reverse proxy when the
// SigV4 gateway is enabled.
type Handler struct {
	keystore  *Keystore
	verifier  *Verifier
	resolver  *CredentialResolver
	target    *TargetResolver
	signer    *v4.Signer
	transport http.RoundTripper
	audit     *slog.Logger
	maxBody   int64
	// now is overridable in tests; defaults to time.Now.
	now func() time.Time
}

// HandlerConfig carries the dependencies needed to build a Handler.
type HandlerConfig struct {
	Keystore  *Keystore
	Verifier  *Verifier
	Resolver  *CredentialResolver
	Target    *TargetResolver
	Transport http.RoundTripper
	MaxBody   int64
}

// NewHandler builds a Handler from its dependencies.
func NewHandler(cfg HandlerConfig) *Handler {
	transport := cfg.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	maxBody := cfg.MaxBody
	if maxBody <= 0 {
		maxBody = 10 << 20 // 10 MiB
	}
	return &Handler{
		keystore:  cfg.Keystore,
		verifier:  cfg.Verifier,
		resolver:  cfg.Resolver,
		target:    cfg.Target,
		signer:    v4.NewSigner(),
		transport: transport,
		audit:     slog.With("component", "sigv4"),
		maxBody:   maxBody,
		now:       time.Now,
	}
}

// shouldStripHeader reports whether an inbound header must be removed before
// re-signing and forwarding: the client's auth-related headers (the outbound
// signer sets its own Authorization / X-Amz-Date / X-Amz-Content-Sha256 /
// X-Amz-Security-Token), forwarding headers, and hop-by-hop headers. Other
// x-amz-* headers (X-Amz-Target, x-amz-meta-*, x-amz-acl, x-amz-copy-source,
// ...) are semantic parts of the AWS request and must pass through so they are
// signed outbound.
func shouldStripHeader(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "x-forwarded-") {
		return true
	}
	switch lower {
	case "x-amz-date", "x-amz-content-sha256", "x-amz-security-token",
		"authorization", "forwarded", "connection", "keep-alive",
		"proxy-authenticate", "proxy-authorization", "te", "trailer",
		"transfer-encoding", "upgrade", "host", "content-length":
		return true
	}
	return false
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := h.now()
	ctx := r.Context()

	// Buffer the (fixed) request body up to the configured limit. A body larger
	// than the limit cannot be verified, so it is rejected.
	body, err := io.ReadAll(io.LimitReader(r.Body, h.maxBody+1))
	_ = r.Body.Close()
	if err != nil {
		writeAWSError(w, http.StatusBadRequest, "InvalidRequest", "Failed to read request body.")
		h.auditReject(r, "", "body_read_error", http.StatusBadRequest, start)
		return
	}
	if int64(len(body)) > h.maxBody {
		writeAWSError(w, http.StatusRequestEntityTooLarge, "EntityTooLarge", "Request body exceeds the configured limit.")
		h.auditReject(r, "", "body_too_large", http.StatusRequestEntityTooLarge, start)
		return
	}

	// 1. Verify the inbound client signature.
	verified, err := h.verifier.Verify(ctx, r, body)
	if err != nil {
		status, code, msg, reason := verificationResponse(err)
		writeAWSError(w, status, code, msg)
		h.auditReject(r, "", reason, status, start)
		return
	}

	ck, ok := h.keystore.Lookup(verified.AccessKeyID)
	if !ok {
		// Key was removed between verification and lookup (e.g. hot reload).
		writeAWSError(w, http.StatusForbidden, "InvalidClientTokenId", "The security token included in the request is invalid.")
		h.auditReject(r, verified.AccessKeyID, "unknown_key", http.StatusForbidden, start)
		return
	}

	// 2. Resolve the outbound target (default or Host-header override).
	target, err := h.target.Resolve(r.Host)
	if err != nil {
		writeAWSError(w, http.StatusBadGateway, "InvalidTarget", "Could not resolve an upstream AWS target for the request.")
		h.auditGatewayReject(r, verified.AccessKeyID, ck.RoleARN, "", "", "target_resolve_error", http.StatusBadGateway, start)
		return
	}

	// 3. Resolve outbound credentials mapped to this client. A credential/STS
	// failure returns a distinct 5xx and never falls back to unsigned or to a
	// different client's role.
	creds, err := h.resolver.Retrieve(ctx, ck)
	if err != nil {
		writeAWSError(w, http.StatusBadGateway, "CredentialResolutionError", "Could not obtain upstream AWS credentials.")
		h.auditGatewayReject(r, verified.AccessKeyID, ck.RoleARN, target.Service, target.Region, "credential_error", http.StatusBadGateway, start)
		return
	}

	// 4. Build the outbound request: fresh, stripped of client auth/forwarding
	// headers, routed to the target, carrying the buffered body.
	outURL := target.Scheme + "://" + target.Host + r.URL.RequestURI()
	outReq, err := http.NewRequestWithContext(ctx, r.Method, outURL, bytes.NewReader(body))
	if err != nil {
		writeAWSError(w, http.StatusBadGateway, "InternalError", "Failed to construct the upstream request.")
		h.auditGatewayReject(r, verified.AccessKeyID, ck.RoleARN, target.Service, target.Region, "build_error", http.StatusBadGateway, start)
		return
	}
	for k, vv := range r.Header {
		if shouldStripHeader(k) {
			continue
		}
		outReq.Header[k] = append([]string(nil), vv...)
	}
	outReq.Host = target.Host
	outReq.ContentLength = int64(len(body))

	// 5. Sign over exactly what will be sent. The payload hash is the SHA-256
	// of the buffered body (fixed-payload signing). S3 requires the
	// X-Amz-Content-Sha256 header to be present and signed, so set it before
	// signing (harmless for non-S3 services; matches the SDK's S3 middleware
	// and awslabs/aws-sigv4-proxy).
	payloadHash := hexSHA256(body)
	outReq.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if err := h.signer.SignHTTP(ctx, creds, outReq, payloadHash, target.Service, target.Region, h.now()); err != nil {
		writeAWSError(w, http.StatusBadGateway, "SigningError", "Failed to sign the upstream request.")
		h.auditGatewayReject(r, verified.AccessKeyID, ck.RoleARN, target.Service, target.Region, "signing_error", http.StatusBadGateway, start)
		return
	}

	// 6. Forward and copy the response back to the client.
	resp, err := h.transport.RoundTrip(outReq)
	if err != nil {
		writeAWSError(w, http.StatusBadGateway, "UpstreamError", "The upstream AWS request failed.")
		h.auditGatewayReject(r, verified.AccessKeyID, ck.RoleARN, target.Service, target.Region, "upstream_error", http.StatusBadGateway, start)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)

	h.auditSuccess(r, verified.AccessKeyID, ck.RoleARN, target.Service, target.Region, resp.StatusCode, start)
}

// copyHeaders copies response headers, skipping hop-by-hop headers.
func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		switch strings.ToLower(k) {
		case "connection", "keep-alive", "proxy-authenticate",
			"proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
			continue
		}
		dst[k] = append([]string(nil), vv...)
	}
}

func latencyMS(start, end time.Time) int64 {
	return end.Sub(start).Milliseconds()
}

func (h *Handler) auditSuccess(r *http.Request, client, role, service, region string, status int, start time.Time) {
	h.audit.Info("request proxied",
		"outcome", "proxied",
		"client", client,
		"role", orNone(role),
		"service", service,
		"region", region,
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"latency_ms", latencyMS(start, h.now()),
	)
}

func (h *Handler) auditReject(r *http.Request, client, reason string, status int, start time.Time) {
	h.audit.Info("request rejected",
		"outcome", "rejected",
		"client", orNone(client),
		"role", "-",
		"service", "-",
		"region", "-",
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"reason", reason,
		"latency_ms", latencyMS(start, h.now()),
	)
}

func (h *Handler) auditGatewayReject(r *http.Request, client, role, service, region, reason string, status int, start time.Time) {
	h.audit.Warn("gateway error",
		"outcome", "gateway_error",
		"client", orNone(client),
		"role", orNone(role),
		"service", orNone(service),
		"region", orNone(region),
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"reason", reason,
		"latency_ms", latencyMS(start, h.now()),
	)
}

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
