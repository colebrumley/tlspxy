package tls

import (
	"crypto/rand"
	cryptotls "crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"

	"github.com/knadh/koanf/v2"
)

// TLSOptions holds parsed TLS version, cipher suite, and ALPN settings.
type TLSOptions struct {
	MinVersion   uint16
	MaxVersion   uint16
	CipherSuites []uint16
	NextProtos   []string
}

// ParseTLSOptions reads TLS option keys from koanf under the given prefix.
func ParseTLSOptions(k *koanf.Koanf, prefix string) (TLSOptions, error) {
	var opts TLSOptions
	var err error

	if opts.MinVersion, err = ParseTLSVersion(k.String(prefix + ".minversion")); err != nil {
		return opts, err
	}
	if opts.MaxVersion, err = ParseTLSVersion(k.String(prefix + ".maxversion")); err != nil {
		return opts, err
	}
	if opts.CipherSuites, err = ParseCipherSuites(k.String(prefix + ".ciphersuites")); err != nil {
		return opts, err
	}

	if alpn := strings.TrimSpace(k.String(prefix + ".alpn")); alpn != "" {
		for _, p := range strings.Split(alpn, ",") {
			if p = strings.TrimSpace(p); p != "" {
				opts.NextProtos = append(opts.NextProtos, p)
			}
		}
	}
	return opts, nil
}

// FileExists checks if a file exists at the given path.
func FileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// SystemCAPool returns a CertPool containing the system's root CA certificates.
func SystemCAPool() (*x509.CertPool, error) {
	return x509.SystemCertPool()
}

// LoadConfigFromFiles takes paths to cert files and loads a Go *tls.Config object.
// The opts parameter allows overriding TLS version, cipher suite, and ALPN settings.
func LoadConfigFromFiles(cert, key, ca string, loadSystemRoots bool, opts TLSOptions) (tlsConf *cryptotls.Config, err error) {
	var (
		tlsCert cryptotls.Certificate
		caPool  *x509.CertPool
		caPem   []byte
	)

	// cert and key must be defined
	if !FileExists(cert) || !FileExists(key) {
		err = errors.New("Could not load cert/key, file does not exist")
		return
	}

	tlsCert, err = cryptotls.LoadX509KeyPair(cert, key)
	if err != nil {
		return
	}

	// Make sure we have a CA somewhere
	if len(ca) == 0 && !loadSystemRoots {
		err = errors.New("Must provide a CA source!")
		return
	}

	caPool = x509.NewCertPool()

	if loadSystemRoots {
		if caPool, err = SystemCAPool(); err != nil {
			return
		}
	}

	if len(ca) > 0 {
		caPem, err = os.ReadFile(ca)
		if err != nil {
			return
		}
		if !caPool.AppendCertsFromPEM(caPem) {
			err = errors.New("Failed to load CA file!")
			return
		}
	}

	minV := opts.MinVersion
	if minV == 0 {
		minV = cryptotls.VersionTLS12
	}

	tlsConf = &cryptotls.Config{
		ClientCAs:    caPool,
		RootCAs:      caPool,
		Rand:         rand.Reader,
		MinVersion:   minV,
		Certificates: []cryptotls.Certificate{tlsCert},
	}
	if opts.MaxVersion != 0 {
		tlsConf.MaxVersion = opts.MaxVersion
	}
	if opts.CipherSuites != nil {
		tlsConf.CipherSuites = opts.CipherSuites
	}
	if opts.NextProtos != nil {
		tlsConf.NextProtos = opts.NextProtos
	}
	return
}
