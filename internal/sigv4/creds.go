package sigv4

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/knadh/koanf/v2"
)

// CredentialResolver maps a verified client identity to the outbound AWS
// credentials used to re-sign the request. It holds a single base credential
// source (static/default/webidentity) and, for clients mapped to a role,
// lazily builds a per-role AssumeRole provider wrapped in an
// aws.CredentialsCache so temporary credentials are reused and refreshed
// before expiry rather than assumed per request.
type CredentialResolver struct {
	mu     sync.Mutex
	base   aws.CredentialsProvider
	stsAPI stscreds.AssumeRoleAPIClient
	cache  map[string]aws.CredentialsProvider
}

// newCredentialResolver constructs a resolver from an explicit base provider
// and STS client. Used directly by tests.
func newCredentialResolver(base aws.CredentialsProvider, stsAPI stscreds.AssumeRoleAPIClient) *CredentialResolver {
	return &CredentialResolver{
		base:   base,
		stsAPI: stsAPI,
		cache:  make(map[string]aws.CredentialsProvider),
	}
}

// NewCredentialResolver builds the resolver from configuration. It fails closed:
// a broken credential source (e.g. unreadable web-identity token file, or an
// unresolvable default chain) returns an error so startup can abort.
func NewCredentialResolver(ctx context.Context, k *koanf.Koanf) (*CredentialResolver, error) {
	region := k.String("sigv4.region")
	source := k.String("sigv4.creds.source")
	if source == "" {
		source = "default"
	}

	var base aws.CredentialsProvider
	switch source {
	case "static":
		base = credentials.NewStaticCredentialsProvider(
			k.String("sigv4.creds.accesskey"),
			k.String("sigv4.creds.secretkey"),
			k.String("sigv4.creds.sessiontoken"),
		)
	case "default":
		cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("loading default AWS credential chain: %w", err)
		}
		base = cfg.Credentials
	case "webidentity":
		// A minimal STS client (no static creds) is sufficient for
		// AssumeRoleWithWebIdentity, which authenticates via the token file.
		stsClient := sts.NewFromConfig(aws.Config{Region: region})
		base = aws.NewCredentialsCache(stscreds.NewWebIdentityRoleProvider(
			stsClient,
			k.String("sigv4.creds.rolearn"),
			stscreds.IdentityTokenFile(k.String("sigv4.creds.tokenfile")),
			func(o *stscreds.WebIdentityRoleOptions) {
				if n := k.String("sigv4.creds.sessionname"); n != "" {
					o.RoleSessionName = n
				}
			},
		))
	default:
		return nil, fmt.Errorf("unknown sigv4.creds.source %q", source)
	}

	// STS client used for per-client AssumeRole, authenticated with the base
	// credentials.
	stsAPI := sts.NewFromConfig(aws.Config{Region: region, Credentials: base})
	return newCredentialResolver(base, stsAPI), nil
}

// providerFor returns the credential provider for a client key, building and
// caching a per-role AssumeRole provider (wrapped in aws.CredentialsCache) the
// first time a role is seen. Clients with no mapped role use the base provider.
func (r *CredentialResolver) providerFor(key ClientKey) aws.CredentialsProvider {
	if key.RoleARN == "" {
		return r.base
	}
	cacheKey := key.RoleARN + "|" + key.ExternalID + "|" + key.SessionName

	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.cache[cacheKey]; ok {
		return p
	}
	arp := stscreds.NewAssumeRoleProvider(r.stsAPI, key.RoleARN, func(o *stscreds.AssumeRoleOptions) {
		if key.SessionName != "" {
			o.RoleSessionName = key.SessionName
		}
		if key.ExternalID != "" {
			o.ExternalID = aws.String(key.ExternalID)
		}
	})
	p := aws.NewCredentialsCache(arp)
	r.cache[cacheKey] = p
	return p
}

// Retrieve returns outbound credentials for the given verified client key.
func (r *CredentialResolver) Retrieve(ctx context.Context, key ClientKey) (aws.Credentials, error) {
	return r.providerFor(key).Retrieve(ctx)
}
