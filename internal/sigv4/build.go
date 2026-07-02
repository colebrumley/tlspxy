package sigv4

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/knadh/koanf/v2"
)

// Gateway bundles the built SigV4 handler with the keystore so the caller can
// wire hot-reload (SIGHUP) and optional autoreload (fsnotify) against it.
type Gateway struct {
	Handler  *Handler
	Keystore *Keystore
}

// Build constructs the full SigV4 gateway from configuration. It fails closed:
// an unreadable/malformed keystore, an invalid target/endpoint, or a broken
// credential source returns an error so startup can abort rather than degrade.
//
// transport is the outbound RoundTripper (typically an *http.Transport carrying
// the backend TLS config); if nil, http.DefaultTransport is used.
func Build(ctx context.Context, k *koanf.Koanf, transport http.RoundTripper, clockSkew time.Duration) (*Gateway, error) {
	ks, err := NewKeystore(k.String("sigv4.keystore"))
	if err != nil {
		return nil, fmt.Errorf("keystore: %w", err)
	}

	target, err := NewTargetResolver(
		k.String("sigv4.service"),
		k.String("sigv4.region"),
		k.String("sigv4.endpoint"),
		k.Bool("sigv4.hostoverride"),
	)
	if err != nil {
		return nil, err
	}

	resolver, err := NewCredentialResolver(ctx, k)
	if err != nil {
		return nil, fmt.Errorf("credentials: %w", err)
	}

	verifier := NewVerifier(ks, clockSkew)

	handler := NewHandler(HandlerConfig{
		Keystore:  ks,
		Verifier:  verifier,
		Resolver:  resolver,
		Target:    target,
		Transport: transport,
		MaxBody:   int64(k.Int("sigv4.maxbodysize")),
	})

	return &Gateway{Handler: handler, Keystore: ks}, nil
}
