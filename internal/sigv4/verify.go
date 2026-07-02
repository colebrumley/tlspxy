package sigv4

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// Sentinel verification errors. The handler maps these to AWS-style responses
// and to audit "reason" values. None of them carry secret material.
var (
	// ErrMalformedAuth means the Authorization header was missing or could not
	// be parsed as an AWS4-HMAC-SHA256 credential.
	ErrMalformedAuth = errors.New("malformed authorization")
	// ErrUnknownKey means the access key ID is not present in the keystore.
	ErrUnknownKey = errors.New("unknown access key")
	// ErrSignatureMismatch means the recomputed signature did not match.
	ErrSignatureMismatch = errors.New("signature mismatch")
	// ErrClockSkew means the request date is outside the allowed skew window.
	ErrClockSkew = errors.New("clock skew exceeded")
	// ErrPayloadMismatch means x-amz-content-sha256 did not match the body.
	ErrPayloadMismatch = errors.New("payload hash mismatch")
	// ErrStreamingUnsupported means a streaming/chunked payload was signed,
	// which is out of scope for this gateway.
	ErrStreamingUnsupported = errors.New("streaming payloads unsupported")
)

// emptyPayloadHash is the hex SHA-256 of an empty body.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// unsignedPayload is the sentinel used for UNSIGNED-PAYLOAD requests.
const unsignedPayload = "UNSIGNED-PAYLOAD"

// authComponents holds the parsed fields of an AWS4-HMAC-SHA256 Authorization
// header.
type authComponents struct {
	AccessKeyID   string
	Date          string // YYYYMMDD from the credential scope
	Region        string
	Service       string
	SignedHeaders []string
	Signature     string
}

// VerifiedRequest is the result of a successful inbound verification.
type VerifiedRequest struct {
	AccessKeyID string
	PayloadHash string // hex hash or "UNSIGNED-PAYLOAD"
}

// Verifier recomputes and checks inbound client SigV4 signatures against the
// keystore. It uses the aws-sdk-go-v2 v4 signer so verification math matches
// genuine SDK-produced signatures.
type Verifier struct {
	keystore  *Keystore
	signer    *v4.Signer
	clockSkew time.Duration
	// now is overridable in tests; defaults to time.Now.
	now func() time.Time
}

// NewVerifier builds a Verifier.
func NewVerifier(ks *Keystore, clockSkew time.Duration) *Verifier {
	return &Verifier{
		keystore:  ks,
		signer:    v4.NewSigner(),
		clockSkew: clockSkew,
		now:       time.Now,
	}
}

// parseAuthHeader parses an AWS4-HMAC-SHA256 Authorization header.
func parseAuthHeader(h string) (authComponents, error) {
	var ac authComponents
	const prefix = "AWS4-HMAC-SHA256 "
	if !strings.HasPrefix(h, prefix) {
		return ac, ErrMalformedAuth
	}
	parts := strings.Split(strings.TrimPrefix(h, prefix), ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "Credential="):
			cred := strings.TrimPrefix(p, "Credential=")
			// AKID/date/region/service/aws4_request
			seg := strings.Split(cred, "/")
			if len(seg) != 5 || seg[4] != "aws4_request" {
				return ac, ErrMalformedAuth
			}
			ac.AccessKeyID = seg[0]
			ac.Date = seg[1]
			ac.Region = seg[2]
			ac.Service = seg[3]
		case strings.HasPrefix(p, "SignedHeaders="):
			ac.SignedHeaders = strings.Split(strings.TrimPrefix(p, "SignedHeaders="), ";")
		case strings.HasPrefix(p, "Signature="):
			ac.Signature = strings.TrimPrefix(p, "Signature=")
		}
	}
	if ac.AccessKeyID == "" || ac.Signature == "" || len(ac.SignedHeaders) == 0 {
		return ac, ErrMalformedAuth
	}
	return ac, nil
}

// signingTime extracts the request signing time from X-Amz-Date (preferred) or
// Date.
func signingTime(r *http.Request) (time.Time, error) {
	if v := r.Header.Get("X-Amz-Date"); v != "" {
		return time.Parse("20060102T150405Z", v)
	}
	if v := r.Header.Get("Date"); v != "" {
		return http.ParseTime(v)
	}
	return time.Time{}, ErrMalformedAuth
}

// Verify fully verifies the inbound request's client signature. body is the
// already-buffered request body (used for the payload-hash check). On success
// it returns the verified identity and the payload hash to use; on failure it
// returns one of the sentinel errors.
func (v *Verifier) Verify(ctx context.Context, r *http.Request, body []byte) (VerifiedRequest, error) {
	var out VerifiedRequest

	ac, err := parseAuthHeader(r.Header.Get("Authorization"))
	if err != nil {
		return out, err
	}

	ck, ok := v.keystore.Lookup(ac.AccessKeyID)
	if !ok {
		return out, ErrUnknownKey
	}

	// Clock-skew check.
	st, err := signingTime(r)
	if err != nil {
		return out, ErrMalformedAuth
	}
	if v.clockSkew > 0 {
		delta := v.now().Sub(st)
		if delta < 0 {
			delta = -delta
		}
		if delta > v.clockSkew {
			return out, ErrClockSkew
		}
	}

	// Payload-hash handling.
	contentHash := r.Header.Get("X-Amz-Content-Sha256")
	var payloadHash string
	switch {
	case contentHash == "":
		// No explicit content hash header: the signed payload hash is the
		// SHA-256 of the body. Compute it for verification.
		payloadHash = hexSHA256(body)
	case strings.HasPrefix(contentHash, "STREAMING-"):
		return out, ErrStreamingUnsupported
	case contentHash == unsignedPayload:
		payloadHash = unsignedPayload
	default:
		// Fixed signed payload: the header must match the actual body.
		if !strings.EqualFold(contentHash, hexSHA256(body)) {
			return out, ErrPayloadMismatch
		}
		payloadHash = contentHash
	}

	// Recompute the signature. Build a clone whose headers are exactly the
	// client's SignedHeaders set, then re-sign with the stored secret using the
	// client's own credential scope (service/region/time). The SDK signer signs
	// all present headers (minus its small ignore list) plus host, so a clone
	// carrying only the signed headers reproduces the client's SignedHeaders
	// string and, given the correct secret, the identical signature.
	clone := r.Clone(ctx)
	clone.Header = make(http.Header)
	clone.Body = nil
	clone.ContentLength = 0
	for _, hn := range ac.SignedHeaders {
		switch strings.ToLower(hn) {
		case "host":
			// Host is signed from clone.Host (preserved by Clone), not the map.
		case "content-length":
			// The SDK signer derives the content-length header from the
			// request's ContentLength field, not the header map. Preserve it
			// only when the client actually signed it.
			clone.ContentLength = r.ContentLength
		default:
			if vals, ok := r.Header[http.CanonicalHeaderKey(hn)]; ok {
				clone.Header[http.CanonicalHeaderKey(hn)] = append([]string(nil), vals...)
			}
		}
	}

	creds := aws.Credentials{
		AccessKeyID:     ac.AccessKeyID,
		SecretAccessKey: ck.SecretKey,
	}
	if err := v.signer.SignHTTP(ctx, creds, clone, payloadHash, ac.Service, ac.Region, st); err != nil {
		return out, ErrSignatureMismatch
	}

	recomputed, err := parseAuthHeader(clone.Header.Get("Authorization"))
	if err != nil {
		return out, ErrSignatureMismatch
	}
	if subtle.ConstantTimeCompare([]byte(recomputed.Signature), []byte(ac.Signature)) != 1 {
		return out, ErrSignatureMismatch
	}

	out.AccessKeyID = ac.AccessKeyID
	out.PayloadHash = payloadHash
	return out, nil
}

// hexSHA256 returns the lowercase hex SHA-256 of b.
func hexSHA256(b []byte) string {
	if len(b) == 0 {
		return emptyPayloadHash
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
