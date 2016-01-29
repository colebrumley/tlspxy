package main

import (
	log "github.com/Sirupsen/logrus"
	syslogrus "github.com/Sirupsen/logrus/hooks/syslog"
	"github.com/olebedev/config"
	"io/ioutil"
	"log/syslog"
	"os"
	"strings"
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
		addr := strings.TrimPrefix(logDest, "syslog://")
		hook, err := syslogrus.NewSyslogHook("udp", addr, syslog.LOG_INFO, "tlspxy")
		if err != nil {
			log.Error("Unable to connect to local syslog daemon")
		} else {
			log.AddHook(hook)
		}
		log.SetOutput(ioutil.Discard)
		return
	}

	log.Debugf("Log Settings: [level: %s, dest: %s]", strings.ToUpper(verbosity), logDest)
}
