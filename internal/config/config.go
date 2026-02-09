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
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/basicflag"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	kfile "github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// ParseConfigPaths extracts -config values from raw args before flag.Parse runs.
func ParseConfigPaths(args []string) []string {
	var paths []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-config" || arg == "--config" {
			if i+1 < len(args) {
				i++
				paths = append(paths, args[i])
			}
		} else if strings.HasPrefix(arg, "-config=") {
			paths = append(paths, strings.TrimPrefix(arg, "-config="))
		} else if strings.HasPrefix(arg, "--config=") {
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
	if err := loadConfigsFromDir(k, dirname); err != nil {
		slog.Warn("Failed to read config from working directory", "error", err)
	}

	// Load config files from extra paths specified via -config flag
	for _, p := range extraPaths {
		info, statErr := os.Stat(p)
		if statErr != nil {
			return nil, fmt.Errorf("config path %q: %w", p, statErr)
		}
		if info.IsDir() {
			if dirErr := loadConfigsFromDir(k, p); dirErr != nil {
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

// loadConfigsFromDir loads all #tlspxy YAML files from a directory into k.
func loadConfigsFromDir(k *koanf.Koanf, dirname string) error {
	files, err := os.ReadDir(dirname)
	if err != nil {
		return err
	}

	for _, f := range files {
		path := filepath.Join(dirname, f.Name())
		if !IsCfgFile(path) {
			continue
		}
		if err := loadConfigFile(k, path); err != nil {
			return err
		}
	}
	return nil
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
	k.Load(env.Provider("TLSPXY_", ".", func(s string) string {
		return strings.Replace(strings.ToLower(strings.TrimPrefix(s, "TLSPXY_")), "_", ".", -1)
	}), nil)
}

// LoadFlags loads CLI flags into k.
// Flag names use dashes as delimiters (e.g., -server-addr for server.addr).
func LoadFlags(k *koanf.Koanf, appVersion, commitID string) {
	// Register flags for all known config keys so flag.Parse recognizes them.
	// Walk the default config to discover all keys and register them with
	// the global flag set using their dash-delimited names.
	RegisterFlags(DefaultConfig, "")

	// Also register the -config flag so flag.Parse doesn't error on it.
	// We already parsed it manually, so this is just to satisfy flag.Parse.
	flag.String("config", "", "Path to config file or directory")

	flag.Usage = func() {
		fmt.Println("Version:       ", appVersion, "| Commit", commitID)
		fmt.Println("Description:    TLSpxy - Tiny TLS termination tool")
		fmt.Println("Usage:          tlspxy [OPTIONS]")
		fmt.Println("Options:")
		PrettyPrintFlagMap(k.Raw())
		fmt.Println("All options can be set via flags, environment variables, or configuration files.",
			"\n  -> See https://github.com/colebrumley/tlspxy/wiki/Configuration for details.")
	}

	flag.Parse()

	k.Load(basicflag.Provider(flag.CommandLine, "."), nil)
}

// RegisterFlags recursively registers flags for all leaf values in the config map.
func RegisterFlags(m map[string]interface{}, prefix string) {
	for key, val := range m {
		flagName := key
		if prefix != "" {
			flagName = prefix + "-" + key
		}
		koanfKey := key
		if prefix != "" {
			koanfKey = strings.Replace(prefix, "-", ".", -1) + "." + key
		}
		switch v := val.(type) {
		case map[string]interface{}:
			RegisterFlags(v, flagName)
		case string:
			flag.String(flagName, v, koanfKey)
		case bool:
			flag.Bool(flagName, v, koanfKey)
		case int:
			flag.Int(flagName, v, koanfKey)
		}
	}
}

// PrettyPrintFlagMap prints flags in a human-readable format for usage output.
func PrettyPrintFlagMap(m map[string]interface{}, prefix ...string) {
	for k, v := range m {
		flagName := "-" + k
		if len(prefix) > 0 {
			flagName = "-" + strings.Join(prefix, "-") + flagName
		}
		switch v.(type) {
		case string, int, bool:
			fmt.Printf("  %s=%+v\n", flagName, v)
		case map[string]interface{}:
			PrettyPrintFlagMap(v.(map[string]interface{}), append(prefix, k)...)
		}
	}
}

// IsCfgFile checks if the file at path is a valid tlspxy config file
// (starts with #tlspxy).
func IsCfgFile(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if scanner.Scan() && scanner.Text() == "#tlspxy" {
		return true
	}
	return false
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
		return fmt.Errorf("server.type: must be one of tcp, http, https; got %q", serverType)
	}

	// 2. remote.addr must be non-empty
	remoteAddr := k.String("remote.addr")
	if remoteAddr == "" {
		return fmt.Errorf("remote.addr: must not be empty")
	}

	// 3. server.addr must be a valid TCP address
	serverAddr := k.String("server.addr")
	if serverAddr == "" {
		serverAddr = ":9898"
	}
	if _, err := net.ResolveTCPAddr("tcp", serverAddr); err != nil {
		return fmt.Errorf("server.addr: invalid TCP address %q: %v", serverAddr, err)
	}

	// 4. TLS cert and key must both be set or both be empty
	tlsCert := k.String("server.tls.cert")
	tlsKey := k.String("server.tls.key")
	if tlsCert != "" && tlsKey == "" {
		return fmt.Errorf("server.tls.key: must be set when server.tls.cert is set")
	}
	if tlsKey != "" && tlsCert == "" {
		return fmt.Errorf("server.tls.cert: must be set when server.tls.key is set")
	}

	// 5. If letsencrypt is enabled, domain must be non-empty
	if k.Bool("server.tls.letsencrypt.enable") {
		domain := k.String("server.tls.letsencrypt.domain")
		if domain == "" {
			return fmt.Errorf("server.tls.letsencrypt.domain: must not be empty when server.tls.letsencrypt.enable is true")
		}
	}

	// 6. For http/https server types, remote.addr must be a valid URL
	if serverType == "http" || serverType == "https" {
		u, err := url.Parse(remoteAddr)
		if err != nil {
			return fmt.Errorf("remote.addr: must be a valid URL for server.type %q: %v", serverType, err)
		}
		if u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("remote.addr: must be a valid URL with scheme and host for server.type %q; got %q", serverType, remoteAddr)
		}
	}

	// 7. Timeout values must be valid Go durations
	for _, key := range []string{"server.timeouts.read", "server.timeouts.write", "server.timeouts.idle"} {
		if v := k.String(key); v != "" {
			if _, err := time.ParseDuration(v); err != nil {
				return fmt.Errorf("%s: invalid duration %q: %v", key, v, err)
			}
		}
	}

	return nil
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
