# tlspxy

A lightweight TLS-terminating TCP and HTTP reverse proxy written in Go.

tlspxy sits in front of your services and handles TLS termination, mutual TLS authentication, and request proxying for both raw TCP and HTTP traffic.

## Features

- **TCP and HTTP/HTTPS** reverse proxy modes
- **Mutual TLS (mTLS)** with client certificate require/verify
- **Let's Encrypt** automatic certificate provisioning
- **Prometheus metrics** for connections, bytes transferred, and errors
- **Health check endpoint** (HTTP mode)
- **Structured logging** via `log/slog` with configurable levels and destinations
- **Graceful shutdown** on SIGINT/SIGTERM
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
  tls:
    cert: ""                 # Path to server TLS certificate
    key: ""                  # Path to server TLS private key
    ca: ""                   # Path to CA cert for client verification
    require: false           # Require client certificates
    verify: false            # Require AND verify client certificates (overrides require)
    letsencrypt:
      enable: false          # Enable automatic Let's Encrypt certificates
      domain: "example.org"  # Domain for the certificate
      cachedir: "/tmp/letsencrypt"  # Certificate cache directory

remote:
  addr: ""                   # Backend address (host:port for TCP, URL for HTTP)
  tls:
    enable: true             # Use TLS when connecting to the backend
    verify: true             # Verify backend certificate
    cert: ""                 # Client certificate for backend mTLS
    key: ""                  # Client key for backend mTLS
    ca: ""                   # Custom CA for backend verification
    sysroots: true           # Include system CA roots

log:
  level: "info"              # Log level: debug, info, warning, error
  contents: false            # Log proxied data content (use with caution)
  destination: "stdout"      # Log destination: stdout, file path, or syslog://address

metrics:
  enable: false              # Enable Prometheus metrics
  addr: ":9090"              # Metrics server listen address
  path: "/metrics"           # Metrics endpoint path
```

### Environment Variables

All environment variables use the `TLSPXY_` prefix to avoid collisions with standard variables (e.g., `REMOTE_ADDR`, `PATH`). The prefix is stripped, then dots are replaced by underscores, all uppercase:

| Config Key | Environment Variable |
|---|---|
| `server.addr` | `TLSPXY_SERVER_ADDR` |
| `server.type` | `TLSPXY_SERVER_TYPE` |
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

Use `-version` to print version and commit info.

## Server TLS

### Certificate and Key

```yaml
server:
  tls:
    cert: "/path/to/server.crt"
    key: "/path/to/server.key"
```

Both `cert` and `key` must be provided together. If neither is set (and Let's Encrypt is disabled), the server runs without TLS.

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

Automatically obtains and renews TLS certificates from Let's Encrypt. The server must be reachable on port 443 for the ACME challenge.

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

Requests to that path return `200 OK` with `{"status":"ok"}`. All other requests are proxied normally.

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
