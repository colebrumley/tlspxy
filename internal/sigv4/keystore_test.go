package sigv4

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestKeystoreLookup(t *testing.T) {
	ks, err := NewKeystore(writeKeystore(t, `keys:
  AKIA1:
    secret: s1
    role_arn: arn:aws:iam::111111111111:role/one
    external_id: ext-1
    session_name: sess-1
  AKIA2:
    secret: s2
`))
	if err != nil {
		t.Fatalf("NewKeystore: %v", err)
	}
	if ks.Len() != 2 {
		t.Fatalf("Len = %d, want 2", ks.Len())
	}
	k1, ok := ks.Lookup("AKIA1")
	if !ok || k1.SecretKey != "s1" || k1.RoleARN != "arn:aws:iam::111111111111:role/one" || k1.ExternalID != "ext-1" || k1.SessionName != "sess-1" {
		t.Errorf("Lookup AKIA1 = %+v, ok=%v", k1, ok)
	}
	k2, ok := ks.Lookup("AKIA2")
	if !ok || k2.SecretKey != "s2" || k2.RoleARN != "" {
		t.Errorf("Lookup AKIA2 = %+v, ok=%v", k2, ok)
	}
	if _, ok := ks.Lookup("nope"); ok {
		t.Errorf("Lookup nope should be absent")
	}
}

func TestNewKeystoreRejectsMissingSecret(t *testing.T) {
	_, err := NewKeystore(writeKeystore(t, `keys:
  AKIA1:
    role_arn: arn:aws:iam::111111111111:role/one
`))
	if err == nil {
		t.Fatal("expected error for entry with no secret")
	}
}

func TestNewKeystoreMissingFile(t *testing.T) {
	if _, err := NewKeystore("/nonexistent/keystore.yml"); err == nil {
		t.Fatal("expected error for missing keystore file")
	}
}

// TestReloadAllFailSafe verifies that a failed reload preserves the previously
// loaded keys (mirrors tls.CertStore fail-safe reload).
func TestReloadAllFailSafe(t *testing.T) {
	path := writeKeystore(t, `keys:
  AKIA1:
    secret: s1
`)
	ks, err := NewKeystore(path)
	if err != nil {
		t.Fatalf("NewKeystore: %v", err)
	}
	// Corrupt the file, then reload: must fail and keep the old key.
	if err := os.WriteFile(path, []byte(":\n  not valid yaml: ["), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := ks.ReloadAll(); err == nil {
		t.Fatal("expected reload error for corrupt file")
	}
	if _, ok := ks.Lookup("AKIA1"); !ok {
		t.Error("failed reload should preserve previously loaded keys")
	}
}

// TestWatchReload covers keystore hot-reload via fsnotify (mirrors
// watcher_test.go). The debounce is set before Watch starts.
func TestWatchReload(t *testing.T) {
	orig := watchDebounce
	watchDebounce = 50 * time.Millisecond
	t.Cleanup(func() { watchDebounce = orig })

	path := writeKeystore(t, `keys:
  AKIA1:
    secret: s1
`)
	ks, err := NewKeystore(path)
	if err != nil {
		t.Fatalf("NewKeystore: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ks.Watch(ctx); err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Add a new key by rewriting the file.
	if err := os.WriteFile(path, []byte(`keys:
  AKIA1:
    secret: s1
  AKIA2:
    secret: s2
    role_arn: arn:aws:iam::222222222222:role/two
`), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := ks.Lookup("AKIA2"); ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for keystore to reload new key")
}
