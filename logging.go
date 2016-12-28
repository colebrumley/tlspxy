package main

import (
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/olebedev/config"
)

func configLogging(cfg *config.Config) {
	// Set verbosity
	verbosity, _ := cfg.String("log.level")
	switch verbosity {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "warning":
		log.SetLevel(log.WarnLevel)
	case "error":
		log.SetLevel(log.ErrorLevel)
	default:
		verbosity = "info"
		log.SetLevel(log.InfoLevel)
	}

	log.SetOutput(os.Stdout)

	logDest, _ := cfg.String("log.destination")
	if len(logDest) == 0 {
		logDest = "stdout"
	}

	if strings.HasPrefix(logDest, "syslog://") {
		if err := syslogging(strings.TrimPrefix(logDest, "syslog://")); err != nil {
			log.Error(err)
			os.Exit(1)
		}
		return
	}

	log.Debugf("Log Settings: [level: %s, dest: %s]", strings.ToUpper(verbosity), logDest)
}
