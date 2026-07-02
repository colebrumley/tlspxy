package tls

import (
	"bytes"
	"context"
	cryptotls "crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setShortDebounce temporarily lowers the watch debounce window for a test and
// restores it on cleanup.
func setShortDebounce(t *testing.T, d time.Duration) {
	t.Helper()
	orig := watchDebounce
	watchDebounce = d
	t.Cleanup(func() { watchDebounce = orig })
}

// certLeafBytes returns the raw DER of the store's current default cert so
// tests can detect an actual swap regardless of pointer identity.
func certLeafBytes(t *testing.T, store *CertStore) []byte {
	t.Helper()
	cert, err := store.GetCertificate(&cryptotls.ClientHelloInfo{})
	if err != nil {
		t.Fatalf("GetCertificate() error = %v", err)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatal("GetCertificate() returned empty cert")
	}
	return cert.Certificate[0]
}

// waitForCertChange polls until the default cert differs from want, or fails
// after timeout.
func waitForCertChange(t *testing.T, store *CertStore, old []byte, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !bytes.Equal(certLeafBytes(t, store), old) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for certificate to change")
}

func newTempStore(t *testing.T) (store *CertStore, tmpCert, tmpKey string) {
	t.Helper()
	root := projectRoot(t)
	tmpDir := t.TempDir()
	tmpCert = filepath.Join(tmpDir, "cert.pem")
	tmpKey = filepath.Join(tmpDir, "key.pem")
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/proxy.crt"), tmpCert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/proxy.key"), tmpKey)

	var err error
	store, err = NewCertStore(tmpCert, tmpKey)
	if err != nil {
		t.Fatalf("NewCertStore() error = %v", err)
	}
	return store, tmpCert, tmpKey
}

func TestWatch_ReloadOnOverwrite(t *testing.T) {
	setShortDebounce(t, 50*time.Millisecond)
	root := projectRoot(t)
	store, tmpCert, tmpKey := newTempStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := store.Watch(ctx); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	old := certLeafBytes(t, store)

	// Overwrite with a different cert/key pair.
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.crt"), tmpCert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.key"), tmpKey)

	waitForCertChange(t, store, old, 3*time.Second)
}

func TestWatch_AtomicRename(t *testing.T) {
	setShortDebounce(t, 50*time.Millisecond)
	root := projectRoot(t)
	store, tmpCert, tmpKey := newTempStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := store.Watch(ctx); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	old := certLeafBytes(t, store)

	// Stage new cert+key in .tmp files, then atomically rename over the
	// originals (certbot / k8s style rotation).
	dir := filepath.Dir(tmpCert)
	stageCert := filepath.Join(dir, "cert.pem.tmp")
	stageKey := filepath.Join(dir, "key.pem.tmp")
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.crt"), stageCert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.key"), stageKey)
	if err := os.Rename(stageKey, tmpKey); err != nil {
		t.Fatalf("rename key: %v", err)
	}
	if err := os.Rename(stageCert, tmpCert); err != nil {
		t.Fatalf("rename cert: %v", err)
	}

	waitForCertChange(t, store, old, 3*time.Second)
}

func TestWatch_BrokenCertPreservesGood(t *testing.T) {
	setShortDebounce(t, 50*time.Millisecond)
	root := projectRoot(t)
	store, tmpCert, tmpKey := newTempStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := store.Watch(ctx); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	good := certLeafBytes(t, store)

	// Write garbage to the cert file. Reload should fail and keep the old cert.
	if err := os.WriteFile(tmpCert, []byte("not a certificate"), 0o644); err != nil {
		t.Fatalf("write broken cert: %v", err)
	}

	// Give the watcher time to observe the event, debounce, and (fail to) reload.
	time.Sleep(500 * time.Millisecond)
	if !bytes.Equal(certLeafBytes(t, store), good) {
		t.Fatal("good certificate should be preserved after a broken write")
	}

	// The watcher must still be running: write a valid new cert and confirm it
	// is picked up.
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.crt"), tmpCert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.key"), tmpKey)
	waitForCertChange(t, store, good, 3*time.Second)
}

func TestWatch_SNIReload(t *testing.T) {
	setShortDebounce(t, 50*time.Millisecond)
	root := projectRoot(t)
	store, _, _ := newTempStore(t)

	tmpDir := t.TempDir()
	sniCert := filepath.Join(tmpDir, "sni.crt")
	sniKey := filepath.Join(tmpDir, "sni.key")
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/proxy.crt"), sniCert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/proxy.key"), sniKey)
	if err := store.AddSNICert("sni.example.com", sniCert, sniKey); err != nil {
		t.Fatalf("AddSNICert() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := store.Watch(ctx); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	sniOld, _ := store.GetCertificate(&cryptotls.ClientHelloInfo{ServerName: "sni.example.com"})
	oldBytes := sniOld.Certificate[0]

	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.crt"), sniCert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.key"), sniKey)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		cur, _ := store.GetCertificate(&cryptotls.ClientHelloInfo{ServerName: "sni.example.com"})
		if !bytes.Equal(cur.Certificate[0], oldBytes) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("SNI certificate was not reloaded")
}

func TestWatch_CancelStopsWatcher(t *testing.T) {
	setShortDebounce(t, 50*time.Millisecond)
	root := projectRoot(t)
	store, tmpCert, tmpKey := newTempStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	if err := store.Watch(ctx); err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	before := certLeafBytes(t, store)

	// Cancel and give the goroutine a moment to exit.
	cancel()
	time.Sleep(200 * time.Millisecond)

	// Modify the files after cancellation; the watcher should not reload.
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.crt"), tmpCert)
	copyFile(t, filepath.Join(root, "contrib/testdata/certs/server.key"), tmpKey)

	time.Sleep(500 * time.Millisecond)
	if !bytes.Equal(certLeafBytes(t, store), before) {
		t.Fatal("watcher reloaded after context cancellation")
	}
}
