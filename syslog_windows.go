package main

import (
	"fmt"
)

func syslogging(addr string) error {
	return fmt.Errorf("Using syslog from Windows is not supported.")
}
