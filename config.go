package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/imdario/mergo"
	"github.com/olebedev/config"
)

func getConfig() (cfg *config.Config, err error) {
	dirname, _ := os.Getwd()
	files, err := ioutil.ReadDir(dirname)
	if err != nil {
		log.Error(err)
	}

	allConfigs := []*config.Config{}
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

		if strings.HasSuffix(f.Name(), ".json") {
			c, err = config.ParseJsonFile(f.Name())
			if err != nil {
				return
			}
			allConfigs = append(allConfigs, c)
		}
	}

	allConfigs = append(allConfigs, &config.Config{Root: DefaultConfig})
	cfg = combineConfigs(allConfigs...)
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
			fmt.Printf("  %s=%+v\n", flagName, v)
		case map[string]interface{}:
			prettyPrintFlagMap(v.(map[string]interface{}), append(prefix, k))
		}
	}
}

func combineConfigs(cfgs ...*config.Config) *config.Config {
	root := map[string]interface{}{}
	for _, conf := range cfgs {
		if err := mergo.Merge(&root, conf.Root.(map[string]interface{})); err != nil {
			log.Error(err)
		}
	}
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
