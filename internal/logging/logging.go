package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/knadh/koanf/v2"
)

// Level is a package-level LevelVar so tests and other code can inspect the
// current logging level programmatically.
var Level = new(slog.LevelVar)

// Configure sets up the logging subsystem based on koanf configuration.
func Configure(k *koanf.Koanf) {
	// Set verbosity
	verbosity := k.String("log.level")
	switch verbosity {
	case "debug":
		Level.Set(slog.LevelDebug)
	case "warning":
		Level.Set(slog.LevelWarn)
	case "error":
		Level.Set(slog.LevelError)
	default:
		verbosity = "info"
		Level.Set(slog.LevelInfo)
	}

	var w io.Writer

	logDest := k.String("log.destination")
	if len(logDest) == 0 {
		logDest = "stdout"
	}

	if strings.HasPrefix(logDest, "syslog://") {
		if err := syslogging(strings.TrimPrefix(logDest, "syslog://")); err != nil {
			fmt.Fprintf(os.Stderr, "syslog error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if logDest != "" && logDest != "stdout" {
		f, err := os.OpenFile(logDest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open log file %s: %v, falling back to stdout\n", logDest, err)
			w = os.Stdout
		} else {
			w = f
		}
	} else {
		w = os.Stdout
	}

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: Level})
	slog.SetDefault(slog.New(handler))
	slog.Debug("Log Settings", "level", strings.ToUpper(verbosity), "dest", logDest)
}
