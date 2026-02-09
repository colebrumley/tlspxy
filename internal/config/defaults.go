package config

// DefaultConfig is the default config object
var DefaultConfig = map[string]interface{}{
	"server": map[string]interface{}{
		"addr":        ":9898",
		"type":        "tcp",
		"healthcheck": "",
		"maxconns":    0, // 0 means unlimited
		"http2":       false,
		"timeouts": map[string]interface{}{
			"read":  "0s",
			"write": "0s",
			"idle":  "300s",
		},
		"tls": map[string]interface{}{
			"verify":       false,
			"require":      false,
			"cert":         "",
			"key":          "",
			"ca":           "",
			"minversion":   "",
			"maxversion":   "",
			"ciphersuites": "",
			"alpn":         "",
			"letsencrypt": map[string]interface{}{
				"enable":   false,
				"domain":   "",
				"cachedir": "/tmp/letsencrypt",
			},
		},
	},
	"remote": map[string]interface{}{
		"addr": "",
		"tls": map[string]interface{}{
			"enable":       true,
			"verify":       true,
			"cert":         "",
			"key":          "",
			"ca":           "",
			"sysroots":     true,
			"minversion":   "",
			"maxversion":   "",
			"ciphersuites": "",
			"alpn":         "",
		},
	},
	"log": map[string]interface{}{
		"level":       "info",
		"contents":    false,
		"destination": "stdout",
	},
	"validate": false,
	"metrics": map[string]interface{}{
		"enable": false,
		"addr":   ":9090",
		"path":   "/metrics",
	},
}
