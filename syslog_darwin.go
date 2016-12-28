package main

import (
	"fmt"
	"io/ioutil"
	"log/syslog"

	log "github.com/Sirupsen/logrus"
	syslogrus "github.com/Sirupsen/logrus/hooks/syslog"
)

func syslogging(addr string) error {
	hook, err := syslogrus.NewSyslogHook("udp", addr, syslog.LOG_INFO, "tlspxy")
	if err != nil {
		return fmt.Errorf("Unable to connect to syslog daemon: %v", err)
	}
	log.AddHook(hook)
	log.SetOutput(ioutil.Discard)
	return nil
}
