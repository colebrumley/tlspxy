package main

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/olebedev/config"
	"io/ioutil"
	"os"
	"strings"
)

var DEFAULT_CONFIG = map[string]interface{}{
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
		"addr": "google.com:443",
		"tls": map[string]interface{}{
			"verify":      true,
			"passthrough": false,
			"cert":        "",
			"key":         "",
			"ca":          "",
			"sysroots":    true,
		},
	},
	"log": map[string]interface{}{
		"level":       "info",
		"contents":    false,
		"destination": "stdout",
	},
}

func getConfig() (cfg *config.Config, err error) {
	dirname, _ := os.Getwd()
	files, err := ioutil.ReadDir(dirname)
	if err != nil {
		log.Error(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".yml") || strings.HasSuffix(f.Name(), ".yaml") {
			cfg, err = config.ParseYamlFile(f.Name())
			break
		}

		if strings.HasSuffix(f.Name(), ".json") {
			cfg, err = config.ParseJsonFile(f.Name())
			break
		}
	}
	if cfg == nil {
		cfg = &config.Config{
			Root: DEFAULT_CONFIG,
		}
	}
	return
}

func prettyPrintFlagMap(m map[string]interface{}, prefix []string) {
	for k, v := range m {
		flagName := "-" + k
		if len(prefix) > 0 {
			flagName = "-" + strings.Join(prefix, "-") + flagName
		}
		switch v.(type) {
		case string, int, bool:
			fmt.Printf("  %s default=%+v\n", flagName, v)
		case map[string]interface{}:
			prettyPrintFlagMap(v.(map[string]interface{}), append(prefix, k))
		}
	}
}
