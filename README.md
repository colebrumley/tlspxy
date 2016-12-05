## Build
Clone the repo and run `make` for a binary or `make docker` for a Docker image. Non-linux (or OSX) distros will need to build manually.

## Run
The binary can be run almost anywhere, including non-glibc distros like Alpine linux.

A systemd template unit is available at `contrib/tlspxy@.service`. See below for available options and configuration methods, and the [service file](https://github.com/colebrumley/tlspxy/blob/master/contrib/tlspxy%40.service) for default settings paths.

### TLS warning
Golang's TLS implementation is [pretty strict](http://www.bite-code.com/2015/06/25/tls-mutual-auth-in-golang/). As a result, you may have to occasionally turn verification off for public websites. For example, attempting to proxy to `google.com:443` with verification on will error with something like:

```
WARN[0039] Connection #001 Remote connection failed: x509: cannot validate certificate for 216.58.219.238 because it doesn't contain any IP SANs
```

In short, if verification is on _everything_ will be verified. IP addresses, SANs, DNS names, all of it.

## Configuration

### Methods
The configuration for `tlspxy` is based on [github.com/olebedev/config](http://godoc.org/github.com/olebedev/config). You can define config options with environment variables, configuration files (YAML/JSON), or flags.

### Loading
`tlspxy` loads its configuration in the following order. Later steps overwrite previous ones:

1. Load the default config template (hard-coded)
2. Load any `.yml` or `.json` files found in the current directory
3. Parse the OS environment
4. Parse command line flags

#### YAML example
Config files for tlspxy _must_ begin with `#tlspxy`. Default options can be omitted. This config will listen on `0.0.0.0:9898` and proxy that connection to `google.com:443`. The remote server's TLS cert will not be verified because of strict IP SAN checking.

```yaml
#tlspxy
log:
  contents: false
  level: debug
remote:
  addr: google.com:443
  tls:
    sysroots: true
    verify: false
```

### Options
Option Path | Environment | Flag | Description
--- | --- | --- | ---
`server.addr` | `SERVER_ADDR` | `-server-addr` | The local server listening address
`server.tls.verify` | `SERVER_TLS_VERIFY` | `-server-tls-verify` | Verify client certs presented to the server
`server.tls.require` | `SERVER_TLS_REQUIRE` | `-server-tls-require` | Require that the client present an x509 cert
`server.tls.cert` | `SERVER_TLS_CERT` | `-server-tls-cert` | The local server's TLS cert
`server.tls.key` | `SERVER_TLS_KEY` | `-server-tls-key` | The local server's TLS key
`server.tls.ca` | `SERVER_TLS_CA` | `-server-tls-ca` | The local server's TLS CA
`server.tls.letsencrypt.enable` | `SERVER_TLS_LETSENCRYPT_ENABLE` | `-server-tls-letsencrypt-enable` | Use LetsEncrypt to generate a trusted certificate
`server.tls.letsencrypt.domain` | `SERVER_TLS_LETSENCRYPT_DOMAIN` | `-server-tls-letsencrypt-domain` | Set the LetsEncrypt domain
`server.tls.letsencrypt.cachedir` | `SERVER_TLS_LETSENCRYPT_CACHEDIR` | `-server-tls-letsencrypt-cachedir` | Set the LetsEncrypt cache directory
`remote.addr` | `REMOTE_ADDR` | `-remote-addr` | Remote server address
`remote.tls.verify` | `REMOTE_TLS_VERIFY` | `-remote-tls-verify` | Verify the remote server's TLS cert
`remote.tls.sysroots` | `REMOTE_TLS_SYSROOTS` | `-remote-tls-sysroots` | Load the system's root CA list
`remote.tls.cert` | `REMOTE_TLS_CERT` | `-remote-tls-cert` | The client cert to present to the remote server
`remote.tls.key` | `REMOTE_TLS_KEY` | `-remote-tls-key` | The key to present to the remote server
`remote.tls.ca` | `REMOTE_TLS_CA` | `-remote-tls-ca` | The CA to present to the remote server
`log.level` | `LOG_LEVEL` | `-log-level` | The log-level to use. Options are `debug`, `info`, `warning`, or `error`. The default is `info`.
`log.contents` | `LOG_CONTENTS` | `-log-contents` | When used in conjunction with `log.level=debug`, prints the raw contents of the TCP stream. If remote TLS is enabled, the output will be encrypted.
`log.destination` | `LOG_DESTINATION` | `-log-destination` | Where to send log output. Options are `stdout` (the default) or `syslog://your-syslog-server` (ex: `syslog://localhost:514`)
