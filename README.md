# tlspxy

A lightweight TLS-terminating TCP and HTTP reverse proxy written in Go.

tlspxy sits in front of your services and handles TLS termination, mutual TLS, and request proxying for both raw TCP and HTTP traffic.

## Features

- TCP and HTTP/HTTPS reverse proxy modes, with HTTP/2 and ALPN
- SNI-based certificate selection and zero-downtime cert reload (SIGHUP or automatic file watching)
- Mutual TLS on both sides: client cert require/verify, backend client certs
- Let's Encrypt automatic certificates
- HAProxy PROXY protocol (v1/v2) to preserve client IPs in TCP mode
- Prometheus metrics, health check endpoint, structured logging
- Single static binary; Docker image included

## Quick Start

Create `config.yaml`:

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

Run it:

```sh
tlspxy -config config.yaml
```

Add `-validate` to check the config and print the resolved settings without starting. `-version` prints version and commit.

### Docker

```sh
make docker
docker run -v /path/to/config.yaml:/etc/tlspxy.yaml \
  -p 8443:8443 \
  elcolio/tlspxy:latest -config /etc/tlspxy.yaml
```

## Configuration

Configuration is loaded in layers, each overriding the previous:

1. Built-in defaults
2. YAML files in the working directory (auto-discovered by `#tlspxy` header)
3. YAML files/directories given via `-config` (a directory loads all its `#tlspxy` files)
4. Environment variables: prefix `TLSPXY_`, dots become underscores, uppercase (`remote.addr` → `TLSPXY_REMOTE_ADDR`)
5. CLI flags: dots become dashes (`remote.addr` → `-remote-addr`)

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
    handshake: "10s"         # Client TLS handshake timeout (TLS listeners only)
  tls:
    cert: ""                 # Path to server TLS certificate
    key: ""                  # Path to server TLS private key
    ca: ""                   # Path to CA cert for client verification
    require: false           # Require client certificates
    verify: false            # Require AND verify client certificates (overrides require)
    autoreload: false        # Watch cert/key files and reload automatically on change
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
  proxyprotocol: ""          # Send HAProxy PROXY protocol header to backend: "", v1, or v2 (TCP mode only)
  timeouts:
    dial: "10s"              # Backend dial (connection establishment) timeout; also bounds backend TLS handshake
  tls:
    enable: true             # Use TLS when connecting to the backend
    verify: true             # Verify backend certificate (false = InsecureSkipVerify; not for production)
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
  contents: false            # Log proxied data content at debug level (very verbose)
  destination: "stdout"      # stdout, /path/to/file, or syslog://address

metrics:
  enable: false              # Enable Prometheus metrics
  addr: ":9090"              # Metrics server listen address
  path: "/metrics"           # Metrics endpoint path

sigv4:                       # AWS SigV4 credential-translation gateway (http/https only)
  enable: false              # Enable the SigV4 gateway
  keystore: ""               # Path to the client access-key keystore YAML file
  autoreload: false          # Watch the keystore file and reload automatically on change
  service: ""                # Default target AWS service (e.g. s3, dynamodb)
  region: ""                 # Default target AWS region (e.g. us-east-1)
  endpoint: ""               # Explicit target endpoint URL (empty => https://<service>.<region>.amazonaws.com)
  hostoverride: false        # Allow the inbound Host header to override the target service/region
  clockskew: "300s"          # Max allowed clock skew for inbound signatures
  maxbodysize: 10485760      # Max inbound body size (bytes) buffered for verification/re-signing
  creds:                     # Outbound base credential source
    source: "default"        # static | default | webidentity
    accesskey: ""            # Static access key ID (source=static)
    secretkey: ""            # Static secret access key (source=static)
    sessiontoken: ""         # Static session token (source=static, optional)
    tokenfile: ""            # Web identity token file (source=webidentity)
    rolearn: ""              # Web identity role ARN (source=webidentity)
    sessionname: "tlspxy"    # Role session name for AssumeRole / web identity
```

Notes:

- **Durations** use Go syntax (`30s`, `5m`); invalid values fail validation at startup. `"0s"` / empty means unbounded or disabled.
- **Cipher suite names** must match Go's `crypto/tls` naming (e.g. `TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384`). TLS 1.3 suites are not configurable — Go always uses the mandatory ones.
- **`server.maxconns`** limits accepted connections. In HTTP/2 mode multiple streams share one connection; use request-level limits if you need per-request control.
- **SNI config is YAML-only** (no flag/env equivalent).

## Server TLS

Set `server.tls.cert` and `server.tls.key` together to terminate TLS. If neither is set (and Let's Encrypt is disabled), the server runs without TLS. If TLS is requested but the cert/key/CA fails to load, startup fails — there is no silent fallback to plaintext.

For TLS listeners, tlspxy completes the client handshake (bounded by `server.timeouts.handshake`) **before** dialing the backend, so bare TCP probes and port scanners never consume a backend connection.

### Certificate Reload

Certificates loaded from files can be rotated with zero downtime — existing connections keep the old cert, new connections get the new one, and a cert that fails to load is ignored in favor of the previous one. Two mechanisms:

- **SIGHUP**: replace the files on disk, then `kill -HUP $(pidof tlspxy)`. Reloads the default and all SNI certificates.
- **Automatic** (`server.tls.autoreload: true`): watches the cert/key files and reloads on change, debounced. Directory-level watching means atomic rename and symlink swaps are detected — recommended for Kubernetes mounted secrets and certbot-style rotation.

Neither applies under Let's Encrypt, which manages its own certificate lifecycle.

### SNI Multi-Domain Certificates

```yaml
server:
  tls:
    cert: "/path/to/default.crt"
    key: "/path/to/default.key"
    sni:
      - hostname: "app.example.com"
        cert: /path/to/app.crt
        key: /path/to/app.key
```

Exact hostname match first, then the default certificate.

### Client Certificates (mTLS)

```yaml
server:
  tls:
    cert: "/path/to/server.crt"
    key: "/path/to/server.key"
    ca: "/path/to/client-ca.crt"
    verify: true
```

`require: true` demands a client certificate without verifying it; `verify: true` demands one that validates against the configured CA (and overrides `require`).

### Let's Encrypt

```yaml
server:
  tls:
    letsencrypt:
      enable: true
      domain: "proxy.example.com"
      cachedir: "/var/cache/letsencrypt"
```

Certificates are obtained and renewed automatically. The server must be reachable on port 443 for the ACME challenge.

## Backend TLS

Backend connections use TLS by default, verified against system roots. Common variations, all under `remote.tls`:

- **Custom CA**: set `ca` to the CA file; set `sysroots: false` to trust only that CA.
- **Backend mTLS**: set `cert` and `key` to present a client certificate.
- **Plaintext backend**: `enable: false`.
- **Skip verification**: `verify: false` (sets `InsecureSkipVerify`; not for production).

Version, cipher suite, and ALPN constraints use the same keys and syntax as the server side.

## Forwarded Headers (HTTP/HTTPS)

The proxy sets `X-Real-IP` to the client peer address, `X-Forwarded-Host` to the inbound `Host`, and `X-Forwarded-Proto` to `https` when the client connection was TLS-terminated.

By default tlspxy treats itself as the trust boundary and **replaces** `X-Forwarded-For` with the real peer, discarding any client-supplied (spoofable) value. Set `server.trustxff: true` only when tlspxy sits behind another trusted proxy — then the peer is appended to the inbound header instead.

## PROXY Protocol (TCP mode)

In TCP mode the backend normally sees only tlspxy's address. Set `remote.proxyprotocol` to `v1` (text) or `v2` (binary) to prepend a HAProxy [PROXY protocol](https://www.haproxy.org/download/1.8/doc/proxy-protocol.txt) header to each backend connection, carrying the real client source and destination addresses:

```yaml
server:
  type: tcp
remote:
  addr: "127.0.0.1:8080"
  proxyprotocol: "v1"
```

The backend must be configured to accept it (nginx `proxy_protocol`, HAProxy `accept-proxy`). TCP mode only; validation rejects it for http/https.

## HTTP/2

```yaml
server:
  type: https
  http2: true
  tls:
    cert: "/path/to/server.crt"
    key: "/path/to/server.key"
    alpn: "h2,http/1.1"
```

Configures both the server and the backend transport for HTTP/2. Requires `server.type` http/https; set `alpn` so clients negotiate `h2`.

## AWS SigV4 Gateway

In HTTP/HTTPS mode, tlspxy can act as an AWS SigV4 credential-translation gateway (`sigv4.enable: true`). Clients sign requests with tlspxy-issued SigV4 access keys; tlspxy fully verifies each signature (recomputing the canonical request, not just checking for auth headers), maps the verified access key to an outbound AWS role, re-signs the request with real AWS credentials it obtains itself, and forwards it to the AWS endpoint. Enabling SigV4 replaces the plain reverse proxy for that listener. It is rejected under `server.type: tcp`.

The client keystore is a separate YAML file (`sigv4.keystore`) mapping each access-key ID to a shared secret and an optional outbound role:

```yaml
keys:
  AKIACLIENTONE:
    secret: "client-one-shared-secret"
    role_arn: "arn:aws:iam::111111111111:role/client-one"   # optional
    external_id: "optional-external-id"                       # optional
    session_name: "optional-session-name"                     # optional
```

The keystore hot-reloads on **SIGHUP**, and automatically when `sigv4.autoreload: true` (directory-level fsnotify watch, debounced, fail-safe — a bad file keeps the previously loaded keys), mirroring the certificate reload mechanism.

**Outbound credentials.** `sigv4.creds.source` selects the base credential source — `static` (`accesskey`/`secretkey`), `default` (IMDS / default provider chain), or `webidentity` (`tokenfile`/`rolearn`, AssumeRoleWithWebIdentity). A keystore entry with a `role_arn` additionally assumes that role via STS on top of the base source; assumed credentials are cached and refreshed before expiry rather than re-assumed per request.

**Routing.** The target defaults to `sigv4.service`/`sigv4.region` (or an explicit `sigv4.endpoint`). With `sigv4.hostoverride: true`, an inbound Host header of the form `<service>.<region>.amazonaws.com` selects that service/region per request.

**Verification.** Requests are rejected (AWS-style XML **403**, no AWS call made) for a missing/malformed signature, unknown access key, signature mismatch, clock skew beyond `sigv4.clockskew`, or a payload hash that does not match the body. Streaming payloads (`STREAMING-AWS4-HMAC-SHA256-PAYLOAD`) are unsupported; fixed signed payloads and `UNSIGNED-PAYLOAD` are supported. Outbound credential/STS failures return a distinct gateway **5xx** and never fall back to unsigned or to another client's role.

**Auditing.** Every request emits one audit line via the standard slog pipeline with `component=sigv4`: client identity, assumed role, target service/region, method, path, status, and latency. Secrets, session tokens, and signatures are never logged or returned in error bodies.

See [`contrib/examples/sigv4-gateway.yml`](contrib/examples/sigv4-gateway.yml).

## Metrics

Enable with `metrics.enable: true` (served on `metrics.addr` at `metrics.path`):

| Metric | Type | Description |
|---|---|---|
| `tlspxy_connections_active` | Gauge | Currently active proxy connections |
| `tlspxy_connections_total` | Counter | Total connections accepted |
| `tlspxy_bytes_sent_total` | Counter | Total bytes sent to the backend |
| `tlspxy_bytes_received_total` | Counter | Total bytes received from the backend |
| `tlspxy_errors_total` | Counter (labeled) | Errors by type (`connection`, `http`) |

## Health Check

In HTTP/HTTPS mode, set `server.healthcheck: "/healthz"` to serve `200 OK` / `{"status":"ok"}` on that path. This is a proxy liveness check — it does not probe the backend. All other paths are proxied normally.

## Logging

Structured logging via `log/slog`. `log.destination` accepts `stdout`, a file path, or `syslog://address` (Linux, macOS, Windows). `log.contents: true` logs proxied payloads at debug level — very verbose, debugging only.

## Examples

Complete configurations in [`contrib/examples/`](contrib/examples/):

| Example | Description |
|---|---|
| [`basic-tcp.yml`](contrib/examples/basic-tcp.yml) | Minimal TCP proxy with TLS termination |
| [`http-reverse-proxy.yml`](contrib/examples/http-reverse-proxy.yml) | HTTP reverse proxy with health check |
| [`letsencrypt.yml`](contrib/examples/letsencrypt.yml) | Let's Encrypt automated certificates |
| [`mutual-tls.yml`](contrib/examples/mutual-tls.yml) | mTLS with client cert verification |
| [`sni-multi-domain.yml`](contrib/examples/sni-multi-domain.yml) | SNI-based multi-domain certs |
| [`http2.yml`](contrib/examples/http2.yml) | HTTP/2 with ALPN and TLS backend |
| [`strict-tls.yml`](contrib/examples/strict-tls.yml) | Hardened TLS 1.3 only |
| [`sigv4-gateway.yml`](contrib/examples/sigv4-gateway.yml) | AWS SigV4 credential-translation gateway |

## Building

Requires **Go 1.24+**.

```sh
make build     # static binary in bin/
make test      # go test -race ./...
make docker    # Docker image
```
