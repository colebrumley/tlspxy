package main

import (
	"fmt"
	"io"
	"log/syslog"

	log "github.com/sirupsen/logrus"
	syslogrus "github.com/sirupsen/logrus/hooks/syslog"
)

func syslogging(addr string) error {
	hook, err := syslogrus.NewSyslogHook("udp", addr, syslog.LOG_INFO, "tlspxy")
	if err != nil {
		return fmt.Errorf("Unable to connect to syslog daemon: %v", err)
	}
	log.AddHook(hook)
	log.SetOutput(io.Discard)
	return nil
}
