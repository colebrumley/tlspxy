package proxy

import (
	"log/slog"
	"net"
	"net/http/httputil"
	"net/url"
)

// NewRewrite builds a Rewrite function for httputil.ReverseProxy that routes
// requests to the backend URL u and manages forwarding headers.
//
// It uses the modern ProxyRequest API (Go 1.20+):
//   - SetURL routes to u's scheme/host and joins u's base path with the inbound
//     path (single-slash join, matching SingleJoiningSlash) while merging any
//     query strings.
//   - SetXForwarded sets X-Forwarded-For, X-Forwarded-Host, and
//     X-Forwarded-Proto on the outbound request. X-Forwarded-Proto reflects
//     whether the inbound connection was TLS.
//
// trustXFF controls X-Forwarded-For handling:
//   - false (default, secure edge behavior): the inbound X-Forwarded-For from
//     the client is discarded and X-Forwarded-For is set to the direct peer IP
//     only. Client-supplied X-Forwarded-Host/Proto are likewise overwritten.
//   - true: the inbound X-Forwarded-For is preserved and the direct peer IP is
//     appended to it. Only enable when tlspxy sits behind a trusted proxy.
//
// X-Real-IP is always set to the direct peer IP.
func NewRewrite(u *url.URL, trustXFF bool) func(*httputil.ProxyRequest) {
	return func(pr *httputil.ProxyRequest) {
		oldURL := pr.In.URL.String()

		// Route to the backend: scheme, host, joined path, merged query.
		pr.SetURL(u)
		// SetURL clears Out.Host so it defaults to the target URL host; set it
		// explicitly to be unambiguous.
		pr.Out.Host = u.Host

		// When trusting an upstream proxy, carry the inbound X-Forwarded-For
		// into the outbound request so SetXForwarded appends to it rather than
		// replacing it. Otherwise SetXForwarded resets it to the direct peer.
		if trustXFF {
			pr.Out.Header["X-Forwarded-For"] = pr.In.Header["X-Forwarded-For"]
		} else {
			pr.Out.Header.Del("X-Forwarded-For")
		}
		pr.SetXForwarded()

		// Always set X-Real-IP to the direct peer, regardless of trust.
		if clientIP, _, err := net.SplitHostPort(pr.In.RemoteAddr); err == nil {
			pr.Out.Header.Set("X-Real-IP", clientIP)
		}

		// Explicitly disable the default Go User-Agent when the client sent none.
		if _, ok := pr.Out.Header["User-Agent"]; !ok {
			pr.Out.Header.Set("User-Agent", "")
		}

		slog.Debug("Rewrote request URL", "component", "http", "from", oldURL, "to", pr.Out.URL.String())
	}
}
