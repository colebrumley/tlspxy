# tlspxy

A lightweight TLS-terminating TCP and HTTP reverse proxy written in Go.

tlspxy sits in front of your services and handles TLS termination, mutual TLS authentication, and request proxying for both raw TCP and HTTP traffic.

## Features

- **TCP and HTTP/HTTPS** reverse proxy modes
- **HTTP/2** support with ALPN negotiation
- **Configurable TLS versions** and cipher suites (server and backend)
- **SNI-based certificate selection** for multi-domain hosting
- **Certificate hot-reload** via SIGHUP (zero-downtime cert rotation)
- **Mutual TLS (mTLS)** with client certificate require/verify
- **Let's Encrypt** automatic certificate provisioning
- **Prometheus metrics** for connections, bytes transferred, and errors
- **Health check endpoint** (HTTP mode)
- **Structured logging** via `log/slog` with configurable levels and destinations
- **Graceful shutdown** on SIGINT/SIGTERM
- **Config validation** with `--validate` flag
- **Docker support** with multi-stage Alpine-based image

## Quick Start

Create a config file `config.yaml`:

```yaml
#tlspxy
server:
  addr: ":8443"
  type: tcp
  tls:
    cert: /path/to/server.crt
    key: /path/to/server.key
remote:
  addr: "127.0.0.1:8080"
  tls:
    enable: false
```

> **Note:** Config files must start with `#tlspxy` on the first line to be auto-discovered.

Run the proxy:

```sh
tlspxy -config config.yaml
```

Validate config without starting:

```sh
tlspxy -config config.yaml -validate
```

### Docker

```sh
# Build
make docker

# Run
docker run -v /path/to/config.yaml:/etc/tlspxy.yaml \
  -p 8443:8443 \
  elcolio/tlspxy:latest -config /etc/tlspxy.yaml
```

## Configuration

Configuration is loaded in layers, with each layer overriding the previous:

1. **Built-in defaults**
2. **YAML files** in the working directory (auto-discovered by `#tlspxy` header)
3. **YAML files/directories** specified via `-config`
4. **Environment variables**
5. **CLI flags**

### Full Config Reference

```yaml
#tlspxy
server:
  addr: ":9898"              # Listen address
  type: "tcp"                # Proxy mode: tcp, http, or https
  healthcheck: ""            # Health check path (HTTP mode only, e.g. "/healthz")
  maxconns: 0                # Max concurrent connections (0 = unlimited)
  http2: false               # Enable HTTP/2 (http/https modes only)
  trustxff: false            # Append to inbound X-Forwarded-For (only enable behind a trusted upstream proxy)
  timeouts:
    read: "0s"               # Read timeout per connection (e.g. 30s, 5m)
    write: "0s"              # Write timeout per connection
    idle: "300s"             # Idle timeout before closing connection
  tls:
    cert: ""                 # Path to server TLS certificate
    key: ""                  # Path to server TLS private key
    ca: ""                   # Path to CA cert for client verification
    require: false           # Require client certificates
    verify: false            # Require AND verify client certificates (overrides require)
    minversion: ""           # Minimum TLS version: 1.0, 1.1, 1.2, 1.3 (default: 1.2)
    maxversion: ""           # Maximum TLS version (default: Go default, currently 1.3)
    ciphersuites: ""         # Comma-separated cipher suite names (default: Go defaults)
    alpn: ""                 # Comma-separated ALPN protocols (e.g. "h2,http/1.1")
    letsencrypt:
      enable: false          # Enable automatic Let's Encrypt certificates
      domain: "example.org"  # Domain for the certificate
      email: ""              # Email for expiry notifications
      cachedir: "/tmp/letsencrypt"  # Certificate cache directory
    sni:                     # SNI-based certificate selection (YAML only)
      - hostname: "app.example.com"
        cert: /path/to/app.crt
        key: /path/to/app.key

remote:
  addr: ""                   # Backend address (host:port for TCP, URL for HTTP)
  tls:
    enable: true             # Use TLS when connecting to the backend
    verify: true             # Verify backend certificate
    cert: ""                 # Client certificate for backend mTLS
    key: ""                  # Client key for backend mTLS
    ca: ""                   # Custom CA for backend verification
    sysroots: true           # Include system CA roots
    minversion: ""           # Minimum TLS version for backend
    maxversion: ""           # Maximum TLS version for backend
    ciphersuites: ""         # Comma-separated cipher suites for backend
    alpn: ""                 # Comma-separated ALPN protocols for backend

log:
  level: "info"              # Log level: debug, info, warning, error
  contents: false            # Log proxied data content (use with caution)
  destination: "stdout"      # Log destination: stdout, file path, or syslog://address

metrics:
  enable: false              # Enable Prometheus metrics
  addr: ":9090"              # Metrics server listen address
  path: "/metrics"           # Metrics endpoint path
```

`server.maxconns` limits accepted TCP connections. In HTTP/2 mode, multiple concurrent streams can share one accepted connection; use backend/request-level limits if you need per-request concurrency control.

### Environment Variables

All environment variables use the `TLSPXY_` prefix to avoid collisions with standard variables (e.g., `REMOTE_ADDR`, `PATH`). The prefix is stripped, then dots are replaced by underscores, all uppercase:

| Config Key | Environment Variable |
|---|---|
| `server.addr` | `TLSPXY_SERVER_ADDR` |
| `server.type` | `TLSPXY_SERVER_TYPE` |
| `server.http2` | `TLSPXY_SERVER_HTTP2` |
| `server.trustxff` | `TLSPXY_SERVER_TRUSTXFF` |
| `server.tls.minversion` | `TLSPXY_SERVER_TLS_MINVERSION` |
| `remote.addr` | `TLSPXY_REMOTE_ADDR` |
| `remote.tls.enable` | `TLSPXY_REMOTE_TLS_ENABLE` |
| `remote.tls.verify` | `TLSPXY_REMOTE_TLS_VERIFY` |
| `log.level` | `TLSPXY_LOG_LEVEL` |
| `metrics.enable` | `TLSPXY_METRICS_ENABLE` |

Any config key follows the same pattern: add the `TLSPXY_` prefix, replace `.` with `_`, and uppercase.

### CLI Flags

Flag names use dashes instead of dots:

```sh
tlspxy \
  -server-addr ":8443" \
  -server-type tcp \
  -remote-addr "127.0.0.1:8080" \
  -remote-tls-enable=false \
  -log-level debug
```

Use `-config` to specify one or more config files or directories:

```sh
tlspxy -config /etc/tlspxy/config.yaml
tlspxy -config /etc/tlspxy.d/           # loads all #tlspxy YAML files in directory
```

Use `-version` to print version and commit info. Use `-validate` to check config and exit.

## Server TLS

### Certificate and Key

```yaml
server:
  tls:
    cert: "/path/to/server.crt"
    key: "/path/to/server.key"
```

Both `cert` and `key` must be provided together. If neither is set (and Let's Encrypt is disabled), the server runs without TLS. If a TLS configuration is requested but fails to load (missing or invalid cert/key/CA), startup fails rather than silently falling back to a plaintext listener.

### TLS Version and Cipher Suites

```yaml
server:
  tls:
    minversion: "1.2"        # Minimum TLS version (1.0, 1.1, 1.2, 1.3)
    maxversion: "1.3"        # Maximum TLS version
    ciphersuites: "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"
```

When left empty, Go defaults are used (TLS 1.2 minimum, system-selected cipher suites). Cipher suite names must match Go's `crypto/tls` naming (e.g., `TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384`). TLS 1.3 cipher suites are not configurable -- Go always uses the mandatory TLS 1.3 suites.

### ALPN Negotiation

```yaml
server:
  tls:
    alpn: "h2,http/1.1"     # Advertised protocols
```

### Certificate Hot-Reload

When TLS certificates are loaded from files (not Let's Encrypt), tlspxy supports zero-downtime certificate rotation via SIGHUP:

```sh
# Replace cert files on disk, then signal the process
kill -HUP $(pidof tlspxy)
```

On SIGHUP, tlspxy reloads the default certificate and all SNI certificates from disk. Existing connections continue using the old certificate; new connections use the reloaded one. If a certificate fails to load, the old certificate is preserved and an error is logged.

### SNI-Based Certificate Selection

Serve different certificates based on the client's requested hostname:

```yaml
server:
  tls:
    cert: "/path/to/default.crt"
    key: "/path/to/default.key"
    sni:
      - hostname: "app.example.com"
        cert: /path/to/app.crt
        key: /path/to/app.key
      - hostname: "api.example.com"
        cert: /path/to/api.crt
        key: /path/to/api.key
```

Lookup order: exact hostname match, then default certificate. SNI configuration is YAML-only (not available via flags or env vars). All SNI certificates are reloaded on SIGHUP alongside the default certificate.

### Client Certificates (mTLS)

```yaml
server:
  tls:
    cert: "/path/to/server.crt"
    key: "/path/to/server.key"
    ca: "/path/to/client-ca.crt"
    require: true    # Require a client cert (any valid cert)
    verify: true     # Require AND verify against the CA (overrides require)
```

- `require: true` -- clients must present a certificate, but it is not verified against the CA
- `verify: true` -- clients must present a certificate that is valid against the configured CA

### Let's Encrypt

```yaml
server:
  tls:
    letsencrypt:
      enable: true
      domain: "proxy.example.com"
      cachedir: "/var/cache/letsencrypt"
```

Automatically obtains and renews TLS certificates from Let's Encrypt. The server must be reachable on port 443 for the ACME challenge. Certificate hot-reload via SIGHUP is not available when using Let's Encrypt (it manages its own certificate lifecycle).

## HTTP/2

Enable HTTP/2 support for HTTP/HTTPS proxy modes:

```yaml
server:
  type: https
  http2: true
  tls:
    cert: "/path/to/server.crt"
    key: "/path/to/server.key"
    alpn: "h2,http/1.1"
```

When `http2: true` is set, both the server and the upstream transport are configured for HTTP/2. This requires `server.type` to be `http` or `https` (not `tcp`). Set `server.tls.alpn` to advertise HTTP/2 support to clients.

## Forwarded Headers (HTTP/HTTPS)

In `http`/`https` mode the proxy always sets `X-Real-IP` to the real client peer address. By default it treats itself as the trust boundary and **replaces** `X-Forwarded-For` with the real peer, discarding any client-supplied value (which is otherwise spoofable). Set `server.trustxff: true` only when tlspxy sits behind another trusted proxy that sets `X-Forwarded-For`; in that case the real peer is appended to the inbound header instead.

## Remote/Backend TLS

### Default (TLS with system roots)

```yaml
remote:
  addr: "backend:443"
  tls:
    enable: true
    verify: true
    sysroots: true
```

### Custom CA

```yaml
remote:
  tls:
    enable: true
    verify: true
    ca: "/path/to/backend-ca.crt"
    sysroots: false
```

### Backend mTLS

```yaml
remote:
  tls:
    enable: true
    cert: "/path/to/client.crt"
    key: "/path/to/client.key"
```

### Backend TLS Version / Cipher Config

```yaml
remote:
  tls:
    enable: true
    minversion: "1.2"
    maxversion: "1.3"
    ciphersuites: "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"
    alpn: "h2,http/1.1"
```

### Skip Verification

```yaml
remote:
  tls:
    enable: true
    verify: false
```

Sets `InsecureSkipVerify: true` on the backend connection. Not recommended for production.

### No TLS

```yaml
remote:
  tls:
    enable: false
```

## Metrics

Enable Prometheus metrics:

```yaml
metrics:
  enable: true
  addr: ":9090"
  path: "/metrics"
```

Available metrics:

| Metric | Type | Description |
|---|---|---|
| `tlspxy_connections_active` | Gauge | Currently active proxy connections |
| `tlspxy_connections_total` | Counter | Total connections accepted |
| `tlspxy_bytes_sent_total` | Counter | Total bytes sent to the backend |
| `tlspxy_bytes_received_total` | Counter | Total bytes received from the backend |
| `tlspxy_errors_total` | Counter (labeled) | Errors by type (`connection`, `http`) |

## Health Check

In HTTP/HTTPS mode, set `server.healthcheck` to a path to enable a health check endpoint:

```yaml
server:
  type: http
  healthcheck: "/healthz"
```

Requests to that path return `200 OK` with `{"status":"ok"}`. This is a proxy liveness check and does not probe the backend. All other requests are proxied normally.

## Logging

```yaml
log:
  level: "info"            # debug, info, warning, error
  destination: "stdout"    # stdout, /path/to/file, or syslog://address
  contents: false          # log proxied data (debug level)
```

Destinations:

- `stdout` -- write to standard output (default)
- `/path/to/file` -- append to the specified file
- `syslog://address` -- send to a syslog server (supported on Linux, macOS, Windows)

Setting `contents: true` logs the actual proxied data at debug level. This generates significant output and should only be used for debugging.

## Examples

See [`contrib/examples/`](contrib/examples/) for complete example configurations:

| Example | Description |
|---|---|
| [`basic-tcp.yml`](contrib/examples/basic-tcp.yml) | Minimal TCP proxy with TLS termination |
| [`http-reverse-proxy.yml`](contrib/examples/http-reverse-proxy.yml) | HTTP reverse proxy with health check |
| [`letsencrypt.yml`](contrib/examples/letsencrypt.yml) | Let's Encrypt automated certificates |
| [`mutual-tls.yml`](contrib/examples/mutual-tls.yml) | mTLS with client cert verification |
| [`sni-multi-domain.yml`](contrib/examples/sni-multi-domain.yml) | SNI-based multi-domain certs |
| [`http2.yml`](contrib/examples/http2.yml) | HTTP/2 with ALPN and TLS backend |
| [`strict-tls.yml`](contrib/examples/strict-tls.yml) | Hardened TLS 1.3 only |

## Building

Requires **Go 1.24+**.

```sh
# Build binary
make build

# Run tests
make test

# Build Docker image
make docker
```

The binary is output to `bin/` and is statically compiled with CGO disabled.
