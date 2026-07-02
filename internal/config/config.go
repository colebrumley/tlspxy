package config

import (
	"bufio"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tlsconfig "github.com/colebrumley/tlspxy/internal/tls"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/basicflag"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	kfile "github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// flagDesc maps flag names (dash-delimited) to human-readable descriptions.
var flagDesc = map[string]string{
	// General
	"config":   "Path to config file or directory",
	"validate": "Validate configuration and print resolved values, then exit",

	// Server
	"server-addr":                     "Listen address (host:port)",
	"server-type":                     "Proxy mode: tcp, http, or https",
	"server-healthcheck":              "Health check path (HTTP mode only, e.g. /healthz)",
	"server-maxconns":                 "Max concurrent connections (0 = unlimited)",
	"server-trustxff":                 "Trust and append inbound X-Forwarded-For (only enable behind a trusted upstream proxy)",
	"server-timeouts-read":            "Read timeout per connection (e.g. 30s, 5m)",
	"server-timeouts-write":           "Write timeout per connection (e.g. 30s, 5m)",
	"server-timeouts-idle":            "Idle timeout before closing connection (e.g. 5m, 1h)",
	"server-timeouts-handshake":       "Client TLS handshake timeout, TLS mode only (e.g. 10s)",
	"server-tls-cert":                 "Path to server TLS certificate",
	"server-tls-key":                  "Path to server TLS private key",
	"server-tls-ca":                   "Path to CA cert for client verification",
	"server-tls-require":              "Require client certificates",
	"server-tls-autoreload":           "Watch cert/key files and reload automatically on change (fsnotify)",
	"server-tls-verify":               "Require and verify client certificates",
	"server-tls-minversion":           "Minimum TLS version (1.0, 1.1, 1.2, 1.3)",
	"server-tls-maxversion":           "Maximum TLS version (1.0, 1.1, 1.2, 1.3)",
	"server-tls-ciphersuites":         "Comma-separated TLS cipher suite names",
	"server-tls-alpn":                 "Comma-separated ALPN protocols (e.g. h2,http/1.1)",
	"server-tls-letsencrypt-enable":   "Enable automatic Let's Encrypt certificates",
	"server-tls-letsencrypt-domain":   "Domain for the Let's Encrypt certificate",
	"server-tls-letsencrypt-cachedir": "Certificate cache directory",
	"server-http2":                    "Enable HTTP/2 support (http/https modes only)",

	// Remote
	"remote-addr":             "Backend address (host:port for TCP, URL for HTTP)",
	"remote-proxyprotocol":    "Send HAProxy PROXY protocol header to backend: '', v1, or v2 (TCP mode only)",
	"remote-timeouts-dial":    "Backend dial (connection establishment) timeout (e.g. 10s)",
	"remote-tls-enable":       "Use TLS when connecting to the backend",
	"remote-tls-verify":       "Verify backend TLS certificate",
	"remote-tls-cert":         "Client certificate for backend mTLS",
	"remote-tls-key":          "Client key for backend mTLS",
	"remote-tls-ca":           "Custom CA cert for backend verification",
	"remote-tls-sysroots":     "Include system CA roots for backend",
	"remote-tls-minversion":   "Minimum TLS version for backend",
	"remote-tls-maxversion":   "Maximum TLS version for backend",
	"remote-tls-ciphersuites": "Comma-separated cipher suites for backend",
	"remote-tls-alpn":         "Comma-separated ALPN protocols for backend",

	// Log
	"log-level":       "Log level: debug, info, warn, error",
	"log-contents":    "Log proxied data content (use with caution)",
	"log-destination": "Log output: stdout, file path, or syslog://address",

	// Metrics
	"metrics-enable": "Enable Prometheus metrics endpoint",
	"metrics-addr":   "Metrics server listen address (host:port)",
	"metrics-path":   "Metrics endpoint path",

	// SigV4 gateway
	"sigv4-enable":             "Enable the AWS SigV4 credential-translation gateway (http/https modes only)",
	"sigv4-keystore":           "Path to the client access-key keystore YAML file",
	"sigv4-autoreload":         "Watch the keystore file and reload automatically on change (fsnotify)",
	"sigv4-service":            "Default target AWS service (e.g. s3, dynamodb)",
	"sigv4-region":             "Default target AWS region (e.g. us-east-1)",
	"sigv4-endpoint":           "Default target endpoint URL (empty = https://<service>.<region>.amazonaws.com)",
	"sigv4-hostoverride":       "Allow the inbound Host header to override the target service/region",
	"sigv4-clockskew":          "Maximum allowed clock skew for inbound signatures (e.g. 5m)",
	"sigv4-maxbodysize":        "Maximum inbound request body size in bytes buffered for verification",
	"sigv4-creds-source":       "Outbound base credential source: static, default, or webidentity",
	"sigv4-creds-accesskey":    "Static outbound AWS access key ID (source=static)",
	"sigv4-creds-secretkey":    "Static outbound AWS secret access key (source=static)",
	"sigv4-creds-sessiontoken": "Static outbound AWS session token (source=static, optional)",
	"sigv4-creds-tokenfile":    "Web identity token file path (source=webidentity)",
	"sigv4-creds-rolearn":      "Web identity role ARN to assume (source=webidentity)",
	"sigv4-creds-sessionname":  "Role session name used for AssumeRole / web identity",
}

// flagGroup defines the display order and grouping for help output.
type flagGroup struct {
	name  string
	flags []string
}

// helpGroups returns the ordered flag groups for the usage output.
func helpGroups() []flagGroup {
	return []flagGroup{
		{
			name: "General",
			flags: []string{
				"config",
				"validate",
			},
		},
		{
			name: "Server",
			flags: []string{
				"server-addr",
				"server-type",
				"server-healthcheck",
				"server-maxconns",
				"server-trustxff",
				"server-http2",
				"server-timeouts-read",
				"server-timeouts-write",
				"server-timeouts-idle",
				"server-timeouts-handshake",
			},
		},
		{
			name: "Server TLS",
			flags: []string{
				"server-tls-cert",
				"server-tls-key",
				"server-tls-ca",
				"server-tls-require",
				"server-tls-verify",
				"server-tls-autoreload",
				"server-tls-minversion",
				"server-tls-maxversion",
				"server-tls-ciphersuites",
				"server-tls-alpn",
				"server-tls-letsencrypt-enable",
				"server-tls-letsencrypt-domain",
				"server-tls-letsencrypt-cachedir",
			},
		},
		{
			name: "Remote",
			flags: []string{
				"remote-addr",
				"remote-proxyprotocol",
				"remote-timeouts-dial",
				"remote-tls-enable",
				"remote-tls-verify",
				"remote-tls-cert",
				"remote-tls-key",
				"remote-tls-ca",
				"remote-tls-sysroots",
				"remote-tls-minversion",
				"remote-tls-maxversion",
				"remote-tls-ciphersuites",
				"remote-tls-alpn",
			},
		},
		{
			name: "Logging",
			flags: []string{
				"log-level",
				"log-contents",
				"log-destination",
			},
		},
		{
			name: "Metrics",
			flags: []string{
				"metrics-enable",
				"metrics-addr",
				"metrics-path",
			},
		},
		{
			name: "SigV4 gateway",
			flags: []string{
				"sigv4-enable",
				"sigv4-keystore",
				"sigv4-autoreload",
				"sigv4-service",
				"sigv4-region",
				"sigv4-endpoint",
				"sigv4-hostoverride",
				"sigv4-clockskew",
				"sigv4-maxbodysize",
				"sigv4-creds-source",
				"sigv4-creds-accesskey",
				"sigv4-creds-secretkey",
				"sigv4-creds-sessiontoken",
				"sigv4-creds-tokenfile",
				"sigv4-creds-rolearn",
				"sigv4-creds-sessionname",
			},
		},
	}
}

// ParseConfigPaths extracts -config values from raw args before flag.Parse runs.
func ParseConfigPaths(args []string) []string {
	var paths []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-config" || arg == "--config":
			if i+1 < len(args) {
				i++
				paths = append(paths, args[i])
			}
		case strings.HasPrefix(arg, "-config="):
			paths = append(paths, strings.TrimPrefix(arg, "-config="))
		case strings.HasPrefix(arg, "--config="):
			paths = append(paths, strings.TrimPrefix(arg, "--config="))
		}
	}
	return paths
}

// GetConfig creates a new koanf instance, loads defaults, then overlays
// YAML config files from the CWD and any extra paths.
func GetConfig(extraPaths ...string) (*koanf.Koanf, error) {
	k := koanf.New(".")

	// Load defaults
	if err := k.Load(confmap.Provider(DefaultConfig, "."), nil); err != nil {
		return nil, fmt.Errorf("loading defaults: %w", err)
	}

	// Load config files from the current working directory
	dirname, _ := os.Getwd()
	if err := loadConfigsFromDir(k, dirname, true); err != nil {
		slog.Warn("Failed to read config from working directory", "error", err)
	}

	// Load config files from extra paths specified via -config flag
	for _, p := range extraPaths {
		info, statErr := os.Stat(p)
		if statErr != nil {
			return nil, fmt.Errorf("config path %q: %w", p, statErr)
		}
		if info.IsDir() {
			if dirErr := loadConfigsFromDir(k, p, false); dirErr != nil {
				return nil, fmt.Errorf("config dir %q: %w", p, dirErr)
			}
		} else {
			if fileErr := loadConfigFile(k, p); fileErr != nil {
				return nil, fmt.Errorf("config file %q: %w", p, fileErr)
			}
		}
	}

	return k, nil
}

// loadConfigsFromDir loads all YAML files (.yml, .yaml) from a directory into k.
// When requireMarker is true (auto-discovery from the working directory), files
// whose first line does not start with the "#tlspxy" marker are skipped, so
// stray YAML (e.g. docker-compose.yml) cannot silently override proxy config.
// When false (a directory passed explicitly via -config), the marker is not
// required and all YAML files are loaded.
func loadConfigsFromDir(k *koanf.Koanf, dirname string, requireMarker bool) error {
	files, err := os.ReadDir(dirname)
	if err != nil {
		return err
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := f.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		path := filepath.Join(dirname, name)
		if requireMarker && !hasMarker(path) {
			continue
		}
		if err := loadConfigFile(k, path); err != nil {
			return err
		}
	}
	return nil
}

// hasMarker reports whether the file at path begins with the "#tlspxy" marker
// on its first line.
func hasMarker(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	line, _ := bufio.NewReader(f).ReadString('\n')
	return strings.HasPrefix(strings.TrimSpace(line), "#tlspxy")
}

// loadConfigFile loads a single YAML config file into k.
func loadConfigFile(k *koanf.Koanf, path string) error {
	if strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml") {
		return k.Load(kfile.Provider(path), yaml.Parser())
	}
	return nil
}

// LoadEnvVars loads environment variables into k.
// Only variables with the TLSPXY_ prefix are read; the prefix is stripped
// before mapping to config keys. For example, TLSPXY_SERVER_ADDR maps to
// server.addr.
func LoadEnvVars(k *koanf.Koanf) {
	if err := k.Load(env.Provider("TLSPXY_", ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, "TLSPXY_")), "_", ".")
	}), nil); err != nil {
		slog.Warn("Failed to load environment variables", "error", err)
	}
}

// LoadFlags loads CLI flags into k.
// Flag names use dashes as delimiters (e.g., -server-addr for server.addr).
func LoadFlags(k *koanf.Koanf, appVersion, commitID string) {
	// Register flags for all known config keys so flag.Parse recognizes them.
	RegisterFlags(DefaultConfig, "")

	// Also register the -config flag so flag.Parse doesn't error on it.
	flag.String("config", "", flagDesc["config"])

	flag.Usage = func() {
		fmt.Println("tlspxy — TLS-terminating TCP and HTTP reverse proxy")
		fmt.Println()
		if appVersion != "" || commitID != "" {
			fmt.Printf("  Version:  %s (commit %s)\n", appVersion, commitID)
			fmt.Println()
		}
		fmt.Println("Usage:")
		fmt.Println("  tlspxy [OPTIONS]")
		fmt.Println()
		printGroupedHelp()
		fmt.Println("Configuration is loaded in layers (each overrides the previous):")
		fmt.Println("  1. Built-in defaults")
		fmt.Println("  2. YAML files in working directory (auto-discovered)")
		fmt.Println("  3. YAML files/directories specified via -config")
		fmt.Println("  4. Environment variables (TLSPXY_ prefix)")
		fmt.Println("  5. CLI flags")
		fmt.Println()
		fmt.Println("See https://github.com/colebrumley/tlspxy for documentation.")
	}

	flag.Parse()

	// Use ProviderWithValue to convert dash-delimited flag names to
	// dot-delimited koanf keys (e.g., "server-addr" → "server.addr").
	if err := k.Load(basicflag.ProviderWithValue(flag.CommandLine, ".", func(key string, value string) (string, interface{}) {
		// Skip the "config" flag — it's handled separately.
		if key == "config" {
			return "", nil
		}
		koanfKey := strings.ReplaceAll(key, "-", ".")
		return koanfKey, value
	}, k), nil); err != nil {
		slog.Warn("Failed to load CLI flags", "error", err)
	}
}

// RegisterFlags recursively registers flags for all leaf values in the config map.
func RegisterFlags(m map[string]interface{}, prefix string) {
	for key, val := range m {
		flagName := key
		if prefix != "" {
			flagName = prefix + "-" + key
		}
		desc := flagDesc[flagName]
		switch v := val.(type) {
		case map[string]interface{}:
			RegisterFlags(v, flagName)
		case string:
			flag.String(flagName, v, desc)
		case bool:
			flag.Bool(flagName, v, desc)
		case int:
			flag.Int(flagName, v, desc)
		}
	}
}

// printGroupedHelp prints flags organized by section with descriptions.
func printGroupedHelp() {
	registered := make(map[string]*flag.Flag)
	flag.VisitAll(func(f *flag.Flag) {
		registered[f.Name] = f
	})

	for _, group := range helpGroups() {
		fmt.Printf("  %s:\n", group.name)
		for _, name := range group.flags {
			desc := flagDesc[name]
			f := registered[name]
			if f != nil {
				defVal := f.DefValue
				if defVal == "" {
					defVal = `""`
				}
				fmt.Printf("    -%-38s %s (default: %s)\n", name, desc, defVal)
			} else {
				fmt.Printf("    -%-38s %s\n", name, desc)
			}
		}
		fmt.Println()
	}
}

// PrettyPrintFlagMap prints flags in a human-readable format for usage output.
func PrettyPrintFlagMap(m map[string]interface{}, prefix ...string) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m[k]
		flagName := "-" + k
		if len(prefix) > 0 {
			flagName = "-" + strings.Join(prefix, "-") + "-" + k
		}
		switch v := v.(type) {
		case string, int, bool:
			fmt.Printf("  %s=%+v\n", flagName, v)
		case map[string]interface{}:
			PrettyPrintFlagMap(v, append(prefix, k)...)
		}
	}
}

// IsCfgFile reports whether path is a YAML config file (.yml or .yaml).
func IsCfgFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yml" && ext != ".yaml" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// ValidateConfig checks that the loaded configuration is valid and returns
// an error describing the first problem found, or nil if everything is OK.
func ValidateConfig(k *koanf.Koanf) error {
	// 1. server.type must be one of tcp, http, https
	serverType := k.String("server.type")
	if serverType == "" {
		serverType = "tcp"
	}
	switch serverType {
	case "tcp", "http", "https":
		// ok
	default:
		return fmt.Errorf("server.type must be one of tcp, http, https; got %q\n  set via: -server-type, TLSPXY_SERVER_TYPE, or server.type in config", serverType)
	}

	// 2. remote.addr must be non-empty
	remoteAddr := k.String("remote.addr")
	if remoteAddr == "" {
		return fmt.Errorf("remote.addr is required\n  set the backend address, e.g. -remote-addr 127.0.0.1:8080\n  or TLSPXY_REMOTE_ADDR, or remote.addr in config")
	}

	// 3. server.addr must be a valid TCP address
	serverAddr := k.String("server.addr")
	if serverAddr == "" {
		serverAddr = ":9898"
	}
	if _, err := net.ResolveTCPAddr("tcp", serverAddr); err != nil {
		return fmt.Errorf("server.addr: invalid TCP address %q: %v\n  expected host:port, e.g. :8443 or 0.0.0.0:8443", serverAddr, err)
	}

	// 4. TLS cert and key must both be set or both be empty
	tlsCert := k.String("server.tls.cert")
	tlsKey := k.String("server.tls.key")
	if tlsCert != "" && tlsKey == "" {
		return fmt.Errorf("server.tls.key is required when server.tls.cert is set\n  set via: -server-tls-key or TLSPXY_SERVER_TLS_KEY")
	}
	if tlsKey != "" && tlsCert == "" {
		return fmt.Errorf("server.tls.cert is required when server.tls.key is set\n  set via: -server-tls-cert or TLSPXY_SERVER_TLS_CERT")
	}

	// 5. If letsencrypt is enabled, domain must be non-empty
	if k.Bool("server.tls.letsencrypt.enable") {
		domain := k.String("server.tls.letsencrypt.domain")
		if domain == "" {
			return fmt.Errorf("server.tls.letsencrypt.domain is required when Let's Encrypt is enabled\n  set via: -server-tls-letsencrypt-domain or TLSPXY_SERVER_TLS_LETSENCRYPT_DOMAIN")
		}
	}
	if serverType == "https" && tlsCert == "" && !k.Bool("server.tls.letsencrypt.enable") {
		return fmt.Errorf("server.type https requires server TLS configuration\n  set server.tls.cert/key or enable server.tls.letsencrypt")
	}

	remoteTLSEnabled := k.Bool("remote.tls.enable")
	remoteTLSCert := k.String("remote.tls.cert")
	remoteTLSKey := k.String("remote.tls.key")
	remoteTLSCA := k.String("remote.tls.ca")
	if remoteTLSCert != "" && remoteTLSKey == "" {
		return fmt.Errorf("remote.tls.key is required when remote.tls.cert is set\n  set via: -remote-tls-key or TLSPXY_REMOTE_TLS_KEY")
	}
	if remoteTLSKey != "" && remoteTLSCert == "" {
		return fmt.Errorf("remote.tls.cert is required when remote.tls.key is set\n  set via: -remote-tls-cert or TLSPXY_REMOTE_TLS_CERT")
	}
	if !remoteTLSEnabled && (remoteTLSCert != "" || remoteTLSKey != "" || remoteTLSCA != "") {
		return fmt.Errorf("remote.tls.enable must be true when remote TLS cert, key, or CA is configured")
	}

	// 6. For http/https server types, remote.addr must be a valid URL
	if serverType == "http" || serverType == "https" {
		u, err := url.Parse(remoteAddr)
		if err != nil {
			return fmt.Errorf("remote.addr must be a valid URL for server.type %q: %v\n  e.g. http://127.0.0.1:8080", serverType, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("remote.addr must include scheme and host for server.type %q; got %q\n  e.g. http://127.0.0.1:8080 or https://backend.example.com", serverType, remoteAddr)
		}
	}

	// 7. Timeout values must be valid Go durations. An empty string is allowed
	// (it means "use the default" or "disabled/unbounded" depending on the key),
	// but a non-empty value that does not parse as a Go duration is a hard error.
	for _, key := range []string{"server.timeouts.read", "server.timeouts.write", "server.timeouts.idle", "server.timeouts.handshake", "remote.timeouts.dial", "sigv4.clockskew"} {
		if v := k.String(key); v != "" {
			if _, err := time.ParseDuration(v); err != nil {
				flagName := strings.ReplaceAll(key, ".", "-")
				return fmt.Errorf("%s: invalid duration %q: %v\n  expected Go duration, e.g. 30s, 5m, 1h\n  set via: -%s", key, v, err, flagName)
			}
		}
	}

	// 8. Normalize "warning" log level to "warn"
	logLevel := k.String("log.level")
	if strings.EqualFold(logLevel, "warning") {
		if err := k.Set("log.level", "warn"); err != nil {
			return fmt.Errorf("normalizing log.level: %w", err)
		}
	}

	// 9. server.http2 is only valid for http/https modes
	if k.Bool("server.http2") && serverType == "tcp" {
		return fmt.Errorf("server.http2 is only valid with server.type http or https, got %q\n  set via: -server-type or server.type in config", serverType)
	}

	// 10. Validate TLS version strings
	for _, key := range []string{
		"server.tls.minversion", "server.tls.maxversion",
		"remote.tls.minversion", "remote.tls.maxversion",
	} {
		if v := k.String(key); v != "" {
			if _, err := tlsconfig.ParseTLSVersion(v); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}

	// 11. Validate cipher suite names
	for _, key := range []string{"server.tls.ciphersuites", "remote.tls.ciphersuites"} {
		if v := k.String(key); v != "" {
			if _, err := tlsconfig.ParseCipherSuites(v); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}

	// 12a. Validate remote.proxyprotocol
	switch pp := k.String("remote.proxyprotocol"); pp {
	case "", "v1", "v2":
		// ok
	default:
		return fmt.Errorf("remote.proxyprotocol must be one of \"\", v1, v2; got %q\n  set via: -remote-proxyprotocol or TLSPXY_REMOTE_PROXYPROTOCOL", pp)
	}
	if k.String("remote.proxyprotocol") != "" && serverType != "tcp" {
		return fmt.Errorf("remote.proxyprotocol is only valid with server.type tcp, got %q\n  PROXY protocol applies to raw TCP backends only", serverType)
	}

	// 13. Validate SigV4 gateway configuration
	if k.Bool("sigv4.enable") {
		if serverType != "http" && serverType != "https" {
			return fmt.Errorf("sigv4.enable is only valid with server.type http or https, got %q\n  the SigV4 gateway requires HTTP-layer request inspection\n  set via: -server-type or server.type in config", serverType)
		}
		if k.String("sigv4.keystore") == "" {
			return fmt.Errorf("sigv4.keystore is required when sigv4.enable is true\n  set the path to the client keystore YAML file\n  set via: -sigv4-keystore or TLSPXY_SIGV4_KEYSTORE")
		}
		switch src := k.String("sigv4.creds.source"); src {
		case "static":
			if k.String("sigv4.creds.accesskey") == "" || k.String("sigv4.creds.secretkey") == "" {
				return fmt.Errorf("sigv4.creds.accesskey and sigv4.creds.secretkey are required when sigv4.creds.source is static")
			}
		case "webidentity":
			if k.String("sigv4.creds.tokenfile") == "" || k.String("sigv4.creds.rolearn") == "" {
				return fmt.Errorf("sigv4.creds.tokenfile and sigv4.creds.rolearn are required when sigv4.creds.source is webidentity")
			}
		case "default", "":
			// ok: IMDS/default credential chain
		default:
			return fmt.Errorf("sigv4.creds.source must be one of static, default, webidentity; got %q\n  set via: -sigv4-creds-source or TLSPXY_SIGV4_CREDS_SOURCE", src)
		}
		if k.String("sigv4.service") == "" && !k.Bool("sigv4.hostoverride") {
			return fmt.Errorf("sigv4.service is required when sigv4.hostoverride is false\n  set the default target service, e.g. -sigv4-service s3")
		}
		if k.String("sigv4.region") == "" && !k.Bool("sigv4.hostoverride") {
			return fmt.Errorf("sigv4.region is required when sigv4.hostoverride is false\n  set the default target region, e.g. -sigv4-region us-east-1")
		}
	}

	// 12. Validate min <= max version
	for _, prefix := range []string{"server.tls", "remote.tls"} {
		minStr := k.String(prefix + ".minversion")
		maxStr := k.String(prefix + ".maxversion")
		if minStr != "" && maxStr != "" {
			minV, _ := tlsconfig.ParseTLSVersion(minStr)
			maxV, _ := tlsconfig.ParseTLSVersion(maxStr)
			if minV > maxV {
				return fmt.Errorf("%s.minversion (%s) must be <= %s.maxversion (%s)", prefix, minStr, prefix, maxStr)
			}
		}
	}

	return nil
}

// Duration parses the duration string at key from k and returns it. Because
// ValidateConfig already rejects non-empty unparseable duration values before
// anything starts, a parse failure here indicates a programmer error (e.g. a
// key that was never validated); in that case it logs a warning and returns 0.
// An empty value returns 0, meaning "use default / disabled" per the caller.
func Duration(k *koanf.Koanf, key string) time.Duration {
	v := k.String(key)
	if v == "" {
		return 0
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("Ignoring unparseable duration (should have failed validation)", "key", key, "value", v, "error", err)
		return 0
	}
	return d
}

// CombineMaps recursively combines n `map[string]interface{}` objects.
// Maps passed later in the list overwrite earlier ones.
func CombineMaps(cfgs ...map[string]interface{}) map[string]interface{} {
	combined := map[string]interface{}{}
	for _, cfg := range cfgs {
		for key, val := range cfg {
			if _, ok := combined[key]; ok {
				switch v := val.(type) {
				default:
					combined[key] = val
				case map[string]interface{}:
					if existing, ok := combined[key].(map[string]interface{}); ok {
						combined[key] = CombineMaps(existing, v)
					} else {
						combined[key] = val
					}
				}
			} else {
				combined[key] = val
			}
		}
	}
	return combined
}
