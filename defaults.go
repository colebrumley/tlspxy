package main

import (
	"github.com/olebedev/config"
)

var (
	cfg *config.Config
	// DefaultConfig is the default config object
	DefaultConfig = map[string]interface{}{
		"server": map[string]interface{}{
			"addr": ":9898",
			"type": "tcp",
			"tls": map[string]interface{}{
				"verify":  false,
				"require": false,
				"cert":    "",
				"key":     "",
				"ca":      "",
				"letsencrypt": map[string]interface{}{
					"enable":   false,
					"domain":   "example.org",
					"cachedir": "/tmp/letsencrypt",
				},
			},
		},
		"remote": map[string]interface{}{
			"addr": "",
			"tls": map[string]interface{}{
				"verify":   true,
				"cert":     "",
				"key":      "",
				"ca":       "",
				"sysroots": true,
			},
		},
		"log": map[string]interface{}{
			"level":       "info",
			"contents":    false,
			"destination": "stdout",
		},
	}
)
