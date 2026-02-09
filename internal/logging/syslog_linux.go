package logging

import (
	"fmt"
	"log/slog"
	"log/syslog"
)

func syslogging(addr string) error {
	w, err := syslog.Dial("udp", addr, syslog.LOG_INFO, "tlspxy")
	if err != nil {
		return fmt.Errorf("unable to connect to syslog daemon: %v", err)
	}
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: Level})
	slog.SetDefault(slog.New(handler))
	return nil
}
