package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/olebedev/config"
)

// parseConfigPaths extracts -config values from raw args before flag.Parse runs.
func parseConfigPaths(args []string) []string {
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

func getConfig(extraPaths ...string) (cfg *config.Config, err error) {
	allConfigs := []*config.Config{{Root: DefaultConfig}}

	// Load config files from the current working directory
	dirname, _ := os.Getwd()
	cwdConfigs, err := loadConfigsFromDir(dirname)
	if err != nil {
		log.Warningf("Failed to read config from working directory: %v", err)
	}
	allConfigs = append(allConfigs, cwdConfigs...)

	// Load config files from extra paths specified via -config flag
	for _, p := range extraPaths {
		info, statErr := os.Stat(p)
		if statErr != nil {
			return nil, fmt.Errorf("config path %q: %w", p, statErr)
		}
		if info.IsDir() {
			dirConfigs, dirErr := loadConfigsFromDir(p)
			if dirErr != nil {
				return nil, fmt.Errorf("config dir %q: %w", p, dirErr)
			}
			allConfigs = append(allConfigs, dirConfigs...)
		} else {
			c, fileErr := loadConfigFile(p)
			if fileErr != nil {
				return nil, fmt.Errorf("config file %q: %w", p, fileErr)
			}
			if c != nil {
				allConfigs = append(allConfigs, c)
			}
		}
	}

	cfg = combineConfigs(allConfigs...)
	return
}

func loadConfigsFromDir(dirname string) ([]*config.Config, error) {
	files, err := os.ReadDir(dirname)
	if err != nil {
		return nil, err
	}

	var configs []*config.Config
	for _, f := range files {
		path := filepath.Join(dirname, f.Name())
		if !isCfgFile(path) {
			continue
		}
		c, err := loadConfigFile(path)
		if err != nil {
			return nil, err
		}
		if c != nil {
			configs = append(configs, c)
		}
	}
	return configs, nil
}

func loadConfigFile(path string) (*config.Config, error) {
	if strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml") {
		return config.ParseYamlFile(path)
	}
	return nil, nil
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
