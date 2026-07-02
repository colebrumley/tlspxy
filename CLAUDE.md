# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

tlspxy is a lightweight TLS-terminating TCP and HTTP/HTTPS reverse proxy written in Go (1.24+). It sits in front of backends and handles TLS termination, mTLS, SNI cert selection, cert hot-reload, and request proxying.

## Commands

```sh
make build       # static build (CGO_ENABLED=0) to bin/tlspxy_<os>_<arch>
make test        # go test -v -race ./...
make bench       # benchmarks in internal/proxy (10s)
make docker      # build image from contrib/Dockerfile
make loadtest    # build the standalone load tester (cmd/loadtest)
make deps        # go mod tidy
```

Run a single test:

```sh
go test -race -run TestName ./internal/proxy/
go test -race -run TestName/SubTest ./internal/config/
```

Lint (config in `.golangci.yml`: govet, errcheck, staticcheck, unused, gosimple, ineffassign, gocritic):

```sh
golangci-lint run
```

Run locally without building: `go run . -config contrib/examples/basic-tcp.yml -validate`. Test fixtures (certs, compose files) live in `contrib/testdata/`.

## Architecture

`main.go` is the entry point and orchestrator. It loads config, builds the TLS listener, then branches on `server.type`:
- **tcp** — manual `Accept()` loop spawning a `proxy.TCPProxy` goroutine per connection (raw byte copying).
- **http/https** — a `httputil.ReverseProxy` wrapping a custom `proxy.Transport`, optionally wrapped by `health.CheckMiddleware`, served via `http.Server`. HTTP/2 is wired with `golang.org/x/net/http2` when `server.http2` is set.

Connection limiting (`server.maxconns`) is implemented differently per mode: a semaphore channel around the Accept loop for TCP, and via `http.Server.ConnState` for HTTP.

### Config layering (`internal/config`)

Built on **koanf**. Precedence, lowest to highest: built-in defaults → YAML files → env vars (`TLSPXY_` prefix, dots→underscores, uppercased) → CLI flags (dots→dashes). `main.go` pre-parses `-config` paths *before* the koanf load so file-specified config can be overridden by env/flags. Auto-discovered YAML files in the working dir must start with a `#tlspxy` first line. `ValidateConfig` runs before anything starts; `-validate` prints resolved config as JSON and exits. SNI config is **YAML-only** (no flag/env equivalent).

### TLS (`internal/tls`)

- `ConfigServer` wraps the inner TCP listener with a TLS listener and returns an optional `certStore`.
- `certstore.go` holds the default + SNI certs and powers hot-reload: on **SIGHUP** `ReloadAll()` re-reads cert files from disk; existing connections keep the old cert, new ones use the reloaded cert, and a failed reload preserves the previous cert. Hot-reload is unavailable under Let's Encrypt (it manages its own lifecycle).
- `ConfigRemote` builds the backend-side `*tls.Config` (custom CA, mTLS client cert, version/cipher/ALPN). `versions.go` maps version/cipher-suite strings to `crypto/tls` constants.

### Signals (`internal/signal`)

`SigHandlerMux` multiplexes OS signals to registered handlers. `main.go` registers SIGHUP→cert reload and SIGINT/SIGTERM→graceful shutdown. Shutdown also flows through a root `context.Context` (`signal.NotifyContext`) that closes the listener / calls `srv.Shutdown`.

### Other packages

- `internal/metrics` — Prometheus counters/gauges (`tlspxy_*`), served on its own listener when `metrics.enable`.
- `internal/logging` — `log/slog` setup; destinations are stdout, a file path, or `syslog://addr` (platform-specific `syslog_{darwin,linux,windows}.go`).
- `internal/health` — health-check middleware for HTTP mode.
- `internal/loadtest` + `cmd/loadtest` — self-contained load-testing harness, generator, and reporting.

## Notes

- The README is the canonical config reference (full YAML schema, env var table, examples in `contrib/examples/`). Keep it in sync when changing config keys.
- Version/commit are injected via ldflags (`main.AppVersion`, `main.CommitID`); bump `VERSION` in the Makefile on release.
