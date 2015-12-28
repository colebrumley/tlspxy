package main

import (
	"github.com/olebedev/config"
)

var (
	cfg            *config.Config
	DEFAULT_CONFIG = map[string]interface{}{
		"server": map[string]interface{}{
			"addr": ":9898",
			"tls": map[string]interface{}{
				"verify":  false,
				"require": false,
				"cert":    "",
				"key":     "",
				"ca":      "",
			},
		},
		"remote": map[string]interface{}{
			"addr": "",
			"tls": map[string]interface{}{
				"verify":      false,
				"passthrough": false,
				"cert":        "",
				"key":         "",
				"ca":          "",
				"sysroots":    false,
			},
		},
		"log": map[string]interface{}{
			"level":       "info",
			"contents":    false,
			"destination": "stdout",
		},
	}
)
