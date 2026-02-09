package tls

import (
	cryptotls "crypto/tls"
	"log/slog"

	"github.com/knadh/koanf/v2"
)

// ConfigRemote reads remote TLS configuration from koanf and returns a *tls.Config.
func ConfigRemote(k *koanf.Koanf) (tlsConf *cryptotls.Config, err error) {
	isTLS := k.Bool("remote.tls.enable")
	cert := k.String("remote.tls.cert")
	key := k.String("remote.tls.key")
	ca := k.String("remote.tls.ca")
	doVerify := k.Bool("remote.tls.verify")
	useSysRoots := k.Bool("remote.tls.sysroots")

	if FileExists(cert) && FileExists(key) {
		slog.Debug("Loading remote TLS config", "cert", cert, "key", key, "ca", ca, "SystemRoots", useSysRoots)
		if tlsConf, err = LoadConfigFromFiles(cert, key, ca, useSysRoots); err != nil {
			return
		}
		slog.Debug("Loading remote TLS config succeeded")
	} else if isTLS {
		tlsConf = &cryptotls.Config{MinVersion: cryptotls.VersionTLS12}
	}

	if doVerify && useSysRoots && tlsConf != nil {
		slog.Debug("Loading default remote TLS config", "verify", doVerify, "system_roots", useSysRoots)
		capool, poolErr := SystemCAPool()
		if poolErr != nil {
			err = poolErr
			return
		}
		tlsConf.RootCAs = capool
		tlsConf.ClientCAs = capool
	}

	if !doVerify && tlsConf != nil {
		tlsConf.InsecureSkipVerify = true
	}
	return
}
