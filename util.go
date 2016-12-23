package main

import (
	"log"
	"net"
	"os"
	"strings"
)

func fileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

// GetOutboundIP Gets preferred outbound ip of this machine
func GetOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().String()
	idx := strings.LastIndex(localAddr, ":")

	return localAddr[0:idx]
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

// combineMaps recursively combines n `map[string]interface{}` objects.
// Maps passed later in the list overwrite earlier ones.
func combineMaps(cfgs ...map[string]interface{}) map[string]interface{} {
	combined := map[string]interface{}{}
	for _, cfg := range cfgs {
		for key, val := range cfg {
			if _, ok := combined[key]; ok {
				switch v := val.(type) {
				default:
					combined[key] = val
				case map[string]interface{}:
					combined[key] = combineMaps(combined[key].(map[string]interface{}), v)
				}
			} else {
				combined[key] = val
			}
		}
	}
	return combined
}
