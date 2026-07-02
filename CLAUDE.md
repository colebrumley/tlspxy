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
- **tcp** — manual `Accept()` loop spawning a `proxy.TCPProxy` goroutine per connection (raw byte copying). For TLS listeners, `Start()` completes the client handshake (bounded by `server.timeouts.handshake`) **before** dialing the backend so bare TCP probes never consume a backend connection; non-TLS listeners skip the gate so server-speaks-first protocols (SMTP, MySQL) work. `remote.proxyprotocol` sends a HAProxy PROXY v1/v2 header (hand-rolled in `proxy/proxyproto.go` — deliberately no third-party dep) as the first bytes on each backend connection.
- **http/https** — a `httputil.ReverseProxy` using the **Rewrite API** (`proxy.NewRewrite` in `proxy/rewrite.go`, not the legacy `Director`) wrapping a custom `proxy.Transport`, optionally wrapped by `health.CheckMiddleware`, served via `http.Server`. HTTP/2 is wired with `golang.org/x/net/http2` when `server.http2` is set.

Connection limiting (`server.maxconns`) is implemented differently per mode: a semaphore channel around the Accept loop for TCP, and via `http.Server.ConnState` for HTTP.

### Config layering (`internal/config`)

Built on **koanf**. Precedence, lowest to highest: built-in defaults → YAML files → env vars (`TLSPXY_` prefix, dots→underscores, uppercased) → CLI flags (dots→dashes). `main.go` pre-parses `-config` paths *before* the koanf load so file-specified config can be overridden by env/flags. Auto-discovered YAML files in the working dir must start with a `#tlspxy` first line. `ValidateConfig` runs before anything starts; `-validate` prints resolved config as JSON and exits. SNI config is **YAML-only** (no flag/env equivalent).

### TLS (`internal/tls`)

- `ConfigServer` wraps the inner TCP listener with a TLS listener and returns an optional `certStore`.
- `certstore.go` holds the default + SNI certs and powers hot-reload: on **SIGHUP** `ReloadAll()` re-reads cert files from disk; existing connections keep the old cert, new ones use the reloaded cert, and a failed reload preserves the previous cert. `watcher.go` adds opt-in automatic reload (`server.tls.autoreload`) via an fsnotify watcher on the cert/key parent directories (watches dirs, not files, so k8s/certbot atomic rename/symlink swaps are caught; events are filtered to the watched filenames + any Create in a watched dir, then debounced ~500ms before calling `ReloadAll`). Hot-reload (both SIGHUP and autoreload) is unavailable under Let's Encrypt (it manages its own lifecycle).
- `ConfigRemote` builds the backend-side `*tls.Config` (custom CA, mTLS client cert, version/cipher/ALPN). `versions.go` maps version/cipher-suite strings to `crypto/tls` constants.

### Signals (`internal/signal`)

`SigHandlerMux` multiplexes OS signals to registered handlers. `main.go` registers SIGHUP→cert reload and SIGINT/SIGTERM→graceful shutdown. Shutdown also flows through a root `context.Context` (`signal.NotifyContext`) that closes the listener / calls `srv.Shutdown`.

### Other packages

- `internal/metrics` — Prometheus counters/gauges (`tlspxy_*`), served on its own listener when `metrics.enable`.
- `internal/logging` — `log/slog` setup; destinations are stdout, a file path, or `syslog://addr` (platform-specific `syslog_{darwin,linux,windows}.go`).
- `internal/health` — health-check middleware for HTTP mode.
- `internal/loadtest` + `cmd/loadtest` — self-contained load-testing harness, generator, and reporting.

## Conventions and gotchas

### Adding a config key (checklist)

Config tests **fail** if any step is skipped: every key needs an entry in `internal/config/defaults.go`, a description in `flagDesc`, and membership in a `helpGroups()` group (`TestFlagDescCompleteness` / `TestHelpGroupsCoversAllFlags` enforce the latter two). Add constraint checks to `ValidateConfig` where applicable, and update the README (YAML schema comment; the config-layering section covers env/flag naming). The README is the canonical user-facing config reference — keep it in sync.

### Durations

Never `time.ParseDuration` with a discarded error in `main.go`. All `server.timeouts.*` and `remote.timeouts.dial` values are validated in `ValidateConfig` (non-empty unparseable → hard error) and resolved via `config.Duration(k, key)` (empty → 0 = unbounded/disabled). New duration keys must be added to the validation loop in `ValidateConfig`.

### HTTP proxy (`proxy/rewrite.go`)

The stdlib strips inbound `Forwarded`/`X-Forwarded-*` from the outbound request **before** calling a `Rewrite` func (unlike `Director`). `trustxff: true` works by copying `pr.In`'s `X-Forwarded-For` into `pr.Out` before `SetXForwarded` so the peer is appended rather than replacing. Don't reintroduce `Director` — `Rewrite` also closes its header-smuggling pitfalls.

### TCPProxy zero-value defaults

`TCPProxy` struct fields are wired from config in `main.go`, but zero values preserve legacy behavior for direct constructors (tests): `DialTimeout` 0 → 30s, `HandshakeTimeout` 0 → 10s. Keep that pattern for new fields — tests construct `TCPProxy` directly without config.

### Other patterns

- Guard all metrics updates with `metrics.Enabled.Load()` — counters are nil until `metrics.Init()`.
- `tls/watcher.go` watches cert **directories**, not files (k8s/certbot atomic rename/symlink swaps kill file-level watches). Any `Create` in a watched dir triggers a debounced reload; spurious reloads are harmless because `ReloadAll` is fail-safe. The debounce is a package var (`watchDebounce`) captured at `Watch()` start — mutating it after start races (tests set it before).
- Startup is fail-closed: requested-but-broken TLS/watcher/config aborts rather than degrading (no silent plaintext fallback).
- Failed client TLS handshakes log at **Info**, not Warn/Error — they're routine (port scanners) and must not spam logs.
- Pre-existing `gofmt -l` drift in `internal/loadtest/report.go`, `scenario.go`, and `internal/tls/remote_test.go`; don't churn them in unrelated changes.

## Releasing

1. Bump `VERSION` in the Makefile (ldflags inject `main.AppVersion`/`main.CommitID`).
2. Commit, tag `vX.Y.Z`, push branch and tag.
3. `gh release create vX.Y.Z --title vX.Y.Z --notes "..."` (releases exist for prior versions; keep the pattern).
