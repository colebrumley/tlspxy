package tls

import (
	"crypto/rand"
	cryptotls "crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

// FileExists checks if a file exists at the given path.
func FileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// SystemCAPool returns a CertPool containing the system's root CA certificates.
func SystemCAPool() (*x509.CertPool, error) {
	return x509.SystemCertPool()
}

// LoadConfigFromFiles takes paths to cert files and loads a Go *tls.Config object
func LoadConfigFromFiles(cert, key, ca string, loadSystemRoots bool) (tlsConf *cryptotls.Config, err error) {
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

	tlsConf = &cryptotls.Config{
		ClientCAs:    caPool,
		RootCAs:      caPool,
		Rand:         rand.Reader,
		MinVersion:   cryptotls.VersionTLS12,
		Certificates: []cryptotls.Certificate{tlsCert},
	}
	return
}
