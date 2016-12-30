package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/olebedev/config"
)

func getConfig() (cfg *config.Config, err error) {
	dirname, _ := os.Getwd()
	files, err := ioutil.ReadDir(dirname)
	if err != nil {
		log.Error(err)
	}

	allConfigs := []*config.Config{{Root: DefaultConfig}}
	for _, f := range files {
		if !isCfgFile(f.Name()) {
			continue
		}
		var c *config.Config
		if strings.HasSuffix(f.Name(), ".yml") || strings.HasSuffix(f.Name(), ".yaml") {
			c, err = config.ParseYamlFile(f.Name())
			if err != nil {
				return
			}
			allConfigs = append(allConfigs, c)
		}
	}

	cfg = combineConfigs(allConfigs...)
	return
}

func prettyPrintFlagMap(m map[string]interface{}, prefix ...string) {
	for k, v := range m {
		flagName := "-" + k
		if len(prefix) > 0 {
			flagName = "-" + strings.Join(prefix, "-") + flagName
		}
		switch v.(type) {
		case string, int, bool:
			fmt.Printf("  %s=%+v\n", flagName, v)
		case map[string]interface{}:
			prettyPrintFlagMap(v.(map[string]interface{}), append(prefix, k)...)
		}
	}
}

// combineConfigs converts n `*config.Config` objects to their underlying
// `map[string]interface{}` objects so we can recursively combine them with
// combineMaps.
func combineConfigs(cfgs ...*config.Config) *config.Config {
	maps := []map[string]interface{}{}
	for _, conf := range cfgs {
		m := append(maps, conf.Root.(map[string]interface{}))
		maps = m
	}
	root := combineMaps(maps...)
	return &config.Config{
		Root: root,
	}
}

func isCfgFile(path string) bool {
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
