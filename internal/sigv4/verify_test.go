package sigv4

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const testURL = "https://s3.us-east-1.amazonaws.com/bucket/key"

func mustKeystore(t *testing.T) *Keystore {
	t.Helper()
	ks, err := NewKeystore(writeKeystore(t, `keys:
  AKIACLIENTONE:
    secret: clientonesecret
    role_arn: arn:aws:iam::111111111111:role/role-one
  AKIACLIENTTWO:
    secret: clienttwosecret
    role_arn: arn:aws:iam::222222222222:role/role-two
`))
	if err != nil {
		t.Fatalf("NewKeystore: %v", err)
	}
	return ks
}

func TestVerifyValidSignature(t *testing.T) {
	ks := mustKeystore(t)
	v := NewVerifier(ks, 5*time.Minute)
	now := time.Now().UTC()
	body := []byte("hello world")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", now, hexSHA256(body))

	vr, err := v.Verify(context.Background(), req, body)
	if err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}
	if vr.AccessKeyID != "AKIACLIENTONE" {
		t.Errorf("AccessKeyID = %q, want AKIACLIENTONE", vr.AccessKeyID)
	}
}

func TestVerifyTamperedSignature(t *testing.T) {
	ks := mustKeystore(t)
	v := NewVerifier(ks, 5*time.Minute)
	now := time.Now().UTC()
	body := []byte("hello world")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", now, hexSHA256(body))

	// Tamper: flip the signature in the Authorization header.
	auth := req.Header.Get("Authorization")
	req.Header.Set("Authorization", strings.Replace(auth, "Signature=", "Signature=0", 1))

	_, err := v.Verify(context.Background(), req, body)
	if !errors.Is(err, ErrSignatureMismatch) && !errors.Is(err, ErrMalformedAuth) {
		t.Fatalf("expected signature mismatch, got %v", err)
	}
}

func TestVerifyTamperedBody(t *testing.T) {
	ks := mustKeystore(t)
	v := NewVerifier(ks, 5*time.Minute)
	now := time.Now().UTC()
	body := []byte("hello world")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", now, hexSHA256(body))

	// The signed content hash no longer matches the presented body.
	tampered := []byte("goodbye world")
	_, err := v.Verify(context.Background(), req, tampered)
	if !errors.Is(err, ErrPayloadMismatch) {
		t.Fatalf("expected payload mismatch, got %v", err)
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	ks := mustKeystore(t)
	v := NewVerifier(ks, 5*time.Minute)
	now := time.Now().UTC()
	body := []byte("data")
	// Client signs with a different secret than the keystore holds.
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "wrongsecret", "s3", "us-east-1", now, hexSHA256(body))

	_, err := v.Verify(context.Background(), req, body)
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected signature mismatch, got %v", err)
	}
}

func TestVerifyUnknownKey(t *testing.T) {
	ks := mustKeystore(t)
	v := NewVerifier(ks, 5*time.Minute)
	now := time.Now().UTC()
	body := []byte("data")
	req := signInbound(t, "PUT", testURL, body, "AKIANOTREAL", "whatever", "s3", "us-east-1", now, hexSHA256(body))

	_, err := v.Verify(context.Background(), req, body)
	if !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("expected unknown key, got %v", err)
	}
}

func TestVerifyClockSkew(t *testing.T) {
	ks := mustKeystore(t)
	v := NewVerifier(ks, 5*time.Minute)
	old := time.Now().UTC().Add(-30 * time.Minute)
	body := []byte("data")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", old, hexSHA256(body))

	_, err := v.Verify(context.Background(), req, body)
	if !errors.Is(err, ErrClockSkew) {
		t.Fatalf("expected clock skew, got %v", err)
	}
}

func TestVerifyUnsignedPayload(t *testing.T) {
	ks := mustKeystore(t)
	v := NewVerifier(ks, 5*time.Minute)
	now := time.Now().UTC()
	body := []byte("streamy but fixed")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", now, unsignedPayload)

	vr, err := v.Verify(context.Background(), req, body)
	if err != nil {
		t.Fatalf("UNSIGNED-PAYLOAD should verify, got %v", err)
	}
	if vr.PayloadHash != unsignedPayload {
		t.Errorf("PayloadHash = %q, want UNSIGNED-PAYLOAD", vr.PayloadHash)
	}
}

func TestVerifyStreamingRejected(t *testing.T) {
	ks := mustKeystore(t)
	v := NewVerifier(ks, 5*time.Minute)
	now := time.Now().UTC()
	body := []byte("chunk")
	req := signInbound(t, "PUT", testURL, body, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", now, unsignedPayload)
	// Force a streaming content hash after signing.
	req.Header.Set("X-Amz-Content-Sha256", "STREAMING-AWS4-HMAC-SHA256-PAYLOAD")

	_, err := v.Verify(context.Background(), req, body)
	if !errors.Is(err, ErrStreamingUnsupported) {
		t.Fatalf("expected streaming unsupported, got %v", err)
	}
}

func TestVerifyMissingAuth(t *testing.T) {
	ks := mustKeystore(t)
	v := NewVerifier(ks, 5*time.Minute)
	req := signInbound(t, "GET", testURL, nil, "AKIACLIENTONE", "clientonesecret", "s3", "us-east-1", time.Now().UTC(), emptyPayloadHash)
	req.Header.Del("Authorization")

	_, err := v.Verify(context.Background(), req, nil)
	if !errors.Is(err, ErrMalformedAuth) {
		t.Fatalf("expected malformed auth, got %v", err)
	}
}
