package sigv4

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	yaml "go.yaml.in/yaml/v3"
)

// ClientKey is a single entry in the keystore: an inbound access-key identity
// and the outbound role it maps to. SecretKey is the shared secret used to
// verify the client's SigV4 signature; RoleARN (if set) is assumed on the
// outbound side. RoleARN may be empty, meaning the request is signed with the
// gateway's base credentials directly.
type ClientKey struct {
	SecretKey   string
	RoleARN     string
	ExternalID  string
	SessionName string
}

// keystoreFile is the on-disk YAML schema:
//
//	keys:
//	  AKIACLIENTONE:
//	    secret: "..."
//	    role_arn: "arn:aws:iam::111111111111:role/example"   # optional
//	    external_id: "..."                                    # optional
//	    session_name: "..."                                   # optional
type keystoreFile struct {
	Keys map[string]struct {
		Secret      string `yaml:"secret"`
		RoleARN     string `yaml:"role_arn"`
		ExternalID  string `yaml:"external_id"`
		SessionName string `yaml:"session_name"`
	} `yaml:"keys"`
}

// Keystore is a thread-safe, hot-reloadable map of client access-key IDs to
// their secret and mapped outbound role. It mirrors the tls.CertStore
// reload/watch pattern: ReloadAll is fail-safe (a failed reload preserves the
// previously loaded keys) and Watch does directory-level fsnotify watching.
type Keystore struct {
	mu   sync.RWMutex
	path string
	keys map[string]ClientKey
}

// parseKeystore reads and parses the keystore file at path.
func parseKeystore(path string) (map[string]ClientKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading keystore: %w", err)
	}
	var kf keystoreFile
	if err := yaml.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("parsing keystore: %w", err)
	}
	keys := make(map[string]ClientKey, len(kf.Keys))
	for id, v := range kf.Keys {
		if id == "" {
			return nil, fmt.Errorf("keystore contains an empty access key ID")
		}
		if v.Secret == "" {
			return nil, fmt.Errorf("keystore entry %q has no secret", id)
		}
		keys[id] = ClientKey{
			SecretKey:   v.Secret,
			RoleARN:     v.RoleARN,
			ExternalID:  v.ExternalID,
			SessionName: v.SessionName,
		}
	}
	return keys, nil
}

// NewKeystore loads the keystore file at path. It fails closed: an
// unreadable or malformed file returns an error so startup can abort.
func NewKeystore(path string) (*Keystore, error) {
	keys, err := parseKeystore(path)
	if err != nil {
		return nil, err
	}
	return &Keystore{path: path, keys: keys}, nil
}

// Lookup returns the ClientKey for the given access key ID, and whether it was
// found.
func (ks *Keystore) Lookup(accessKeyID string) (ClientKey, bool) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	ck, ok := ks.keys[accessKeyID]
	return ck, ok
}

// Len returns the number of loaded keys.
func (ks *Keystore) Len() int {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return len(ks.keys)
}

// ReloadAll re-reads the keystore file from disk. On any failure the previously
// loaded keys are preserved and the error is returned (fail-safe, mirroring
// tls.CertStore.ReloadAll).
func (ks *Keystore) ReloadAll() error {
	keys, err := parseKeystore(ks.path)
	if err != nil {
		return fmt.Errorf("reloading keystore: %w", err)
	}
	ks.mu.Lock()
	ks.keys = keys
	ks.mu.Unlock()
	return nil
}

// watchDebounce coalesces bursts of filesystem events before triggering a
// keystore reload. Overridable in tests (set before calling Watch; mutating it
// after Watch starts races with the watcher goroutine).
var watchDebounce = 500 * time.Millisecond

// Watch starts a goroutine that watches the directory containing the keystore
// file and calls ReloadAll when it changes. It returns once the watcher is
// established; the goroutine runs until ctx is cancelled. Directories (not the
// file itself) are watched so atomic rename/symlink swaps used by Kubernetes
// mounted secrets are detected. Reloads are debounced and fail-safe.
func (ks *Keystore) Watch(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating fsnotify watcher: %w", err)
	}

	path := ks.path
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	} else {
		path = filepath.Clean(path)
	}
	dir := filepath.Dir(path)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return fmt.Errorf("watching directory %s: %w", dir, err)
	}

	debounce := watchDebounce
	go ks.watchLoop(ctx, watcher, path, dir, debounce)
	return nil
}

func (ks *Keystore) watchLoop(ctx context.Context, watcher *fsnotify.Watcher, watchedFile, dir string, debounce time.Duration) {
	defer watcher.Close()

	timer := time.NewTimer(debounce)
	if !timer.Stop() {
		<-timer.C
	}
	timerActive := false

	for {
		select {
		case <-ctx.Done():
			slog.Debug("Keystore watcher stopping", "reason", ctx.Err())
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !relevantEvent(event, watchedFile, dir) {
				continue
			}
			slog.Debug("Keystore file event", "op", event.Op.String(), "name", event.Name)
			if timerActive && !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounce)
			timerActive = true

		case <-timer.C:
			timerActive = false
			if err := ks.ReloadAll(); err != nil {
				slog.Error("Automatic keystore reload failed", "error", err)
			} else {
				slog.Info("Keystore reloaded automatically", "keys", ks.Len())
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("Keystore watcher error", "error", err)
		}
	}
}

// relevantEvent reports whether a filesystem event should trigger a reload:
// an event on the watched file, or any Create inside the watched directory
// (to catch symlink swaps that only touch a sibling name).
func relevantEvent(event fsnotify.Event, watchedFile, dir string) bool {
	name := event.Name
	if abs, err := filepath.Abs(name); err == nil {
		name = abs
	} else {
		name = filepath.Clean(name)
	}
	if name == watchedFile {
		return true
	}
	if event.Op&fsnotify.Create != 0 && filepath.Dir(name) == dir {
		return true
	}
	return false
}
