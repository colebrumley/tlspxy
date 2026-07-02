package sigv4

import (
	"fmt"
	"net/url"
	"strings"
)

// Target describes where a re-signed request is sent and how it is signed.
type Target struct {
	Service string
	Region  string
	Scheme  string
	Host    string // host[:port] of the upstream endpoint
}

// TargetResolver determines the outbound AWS target for a request. It uses a
// configured default (service/region/endpoint) and, when host override is
// enabled, parses the inbound Host header of the form
// <service>.<region>.amazonaws.com to route/sign for that service and region.
type TargetResolver struct {
	defaultService string
	defaultRegion  string
	defaultScheme  string
	defaultHost    string
	hostOverride   bool
}

// NewTargetResolver builds a resolver. endpoint may be empty, in which case the
// default endpoint is derived as https://<service>.<region>.amazonaws.com.
func NewTargetResolver(service, region, endpoint string, hostOverride bool) (*TargetResolver, error) {
	tr := &TargetResolver{
		defaultService: service,
		defaultRegion:  region,
		hostOverride:   hostOverride,
	}
	if endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parsing sigv4.endpoint %q: %w", endpoint, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return nil, fmt.Errorf("sigv4.endpoint must include scheme and host; got %q", endpoint)
		}
		tr.defaultScheme = u.Scheme
		tr.defaultHost = u.Host
	} else if service != "" && region != "" {
		tr.defaultScheme = "https"
		tr.defaultHost = fmt.Sprintf("%s.%s.amazonaws.com", service, region)
	}
	return tr, nil
}

// Resolve returns the Target for a request. host is the inbound Host header.
// When host override is enabled and the host looks like an AWS endpoint, the
// service/region/endpoint are taken from it; otherwise the configured default
// is used.
func (tr *TargetResolver) Resolve(host string) (Target, error) {
	if tr.hostOverride && host != "" {
		if svc, region, ok := parseAWSHost(host); ok {
			return Target{
				Service: svc,
				Region:  region,
				Scheme:  "https",
				Host:    stripPort(host),
			}, nil
		}
	}
	if tr.defaultHost == "" {
		return Target{}, fmt.Errorf("no default target configured and Host %q is not an AWS endpoint", host)
	}
	return Target{
		Service: tr.defaultService,
		Region:  tr.defaultRegion,
		Scheme:  tr.defaultScheme,
		Host:    tr.defaultHost,
	}, nil
}

// parseAWSHost parses an AWS endpoint host of the form
// <service>.<region>.amazonaws.com (or the global <service>.amazonaws.com,
// which is treated as region us-east-1). It returns the service, region, and
// whether the host was recognized as an AWS endpoint.
func parseAWSHost(host string) (service, region string, ok bool) {
	h := stripPort(host)
	const suffix = ".amazonaws.com"
	if !strings.HasSuffix(h, suffix) {
		return "", "", false
	}
	prefix := strings.TrimSuffix(h, suffix)
	parts := strings.Split(prefix, ".")
	switch len(parts) {
	case 1:
		// Global endpoint, e.g. iam.amazonaws.com / sts.amazonaws.com.
		return parts[0], "us-east-1", true
	default:
		// <service>.<region>[.<extra>...].amazonaws.com — service is first,
		// region is second.
		return parts[0], parts[1], true
	}
}

// stripPort removes a trailing :port from host if present.
func stripPort(host string) string {
	if i := strings.LastIndex(host, ":"); i != -1 {
		// Guard against IPv6 without port; AWS hosts are DNS names so this is
		// safe, but keep it simple.
		return host[:i]
	}
	return host
}
