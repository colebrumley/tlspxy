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

	opts, err := ParseTLSOptions(k, "remote.tls")
	if err != nil {
		return nil, err
	}

	if FileExists(cert) && FileExists(key) {
		slog.Debug("Loading remote TLS config", "cert", cert, "key", key, "ca", ca, "SystemRoots", useSysRoots)
		if tlsConf, err = LoadConfigFromFiles(cert, key, ca, useSysRoots, opts); err != nil {
			return
		}
		slog.Debug("Loading remote TLS config succeeded")
	} else if isTLS {
		minV := opts.MinVersion
		if minV == 0 {
			minV = cryptotls.VersionTLS12
		}
		tlsConf = &cryptotls.Config{MinVersion: minV}
		if opts.MaxVersion != 0 {
			tlsConf.MaxVersion = opts.MaxVersion
		}
		if opts.CipherSuites != nil {
			tlsConf.CipherSuites = opts.CipherSuites
		}
		if opts.NextProtos != nil {
			tlsConf.NextProtos = opts.NextProtos
		}
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
