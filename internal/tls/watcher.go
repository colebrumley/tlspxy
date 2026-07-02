package tls

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchDebounce is the window used to coalesce bursts of filesystem events
// before triggering a certificate reload. Overridable in tests.
var watchDebounce = 500 * time.Millisecond

// certFilePaths returns the set of all cert and key file paths the store
// depends on (default cert/key plus every SNI entry), cleaned to absolute
// form where possible.
func (cs *CertStore) certFilePaths() []string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	paths := make([]string, 0, 2+2*len(cs.sniEntries))
	paths = append(paths, cs.certPath, cs.keyPath)
	for _, entry := range cs.sniEntries {
		paths = append(paths, entry.CertPath, entry.KeyPath)
	}

	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil {
			out = append(out, abs)
		} else {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

// Watch starts a goroutine that watches the directories containing the
// store's certificate and key files and automatically calls ReloadAll when
// they change. It returns after the watcher is established; the goroutine
// runs until ctx is cancelled, at which point the underlying fsnotify watcher
// is closed.
//
// Directories (not the files themselves) are watched so that atomic
// rename/symlink swaps used by Kubernetes mounted secrets and certbot-style
// rotation are detected even after the original inode disappears. Events are
// filtered to the watched filenames, and Create events anywhere in a watched
// directory also trigger a reload (to catch symlink swaps that only touch a
// sibling name). Reloads are debounced and fail-safe: a failed reload keeps
// the previously loaded certificate and never stops the watcher.
func (cs *CertStore) Watch(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating fsnotify watcher: %w", err)
	}

	// Build the set of watched file paths and the deduplicated set of parent
	// directories to add to the watcher.
	files := cs.certFilePaths()
	watchedFiles := make(map[string]struct{}, len(files))
	dirs := make(map[string]struct{})
	for _, f := range files {
		watchedFiles[f] = struct{}{}
		dirs[filepath.Dir(f)] = struct{}{}
	}

	for dir := range dirs {
		if err := watcher.Add(dir); err != nil {
			watcher.Close()
			return fmt.Errorf("watching directory %s: %w", dir, err)
		}
	}

	// Capture the debounce window once so the goroutine never races with a
	// test mutating the package-level default.
	debounce := watchDebounce

	go cs.watchLoop(ctx, watcher, watchedFiles, dirs, debounce)
	return nil
}

// watchLoop is the event pump for Watch. It runs until ctx is cancelled.
func (cs *CertStore) watchLoop(ctx context.Context, watcher *fsnotify.Watcher, watchedFiles, dirs map[string]struct{}, debounce time.Duration) {
	defer watcher.Close()

	// Debounce timer, created stopped. We reuse a single timer and reset it
	// on each relevant event.
	timer := time.NewTimer(debounce)
	if !timer.Stop() {
		<-timer.C
	}
	timerActive := false

	for {
		select {
		case <-ctx.Done():
			slog.Debug("Certificate watcher stopping", "reason", ctx.Err())
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !cs.relevantEvent(event, watchedFiles, dirs) {
				continue
			}
			slog.Debug("Certificate file event", "op", event.Op.String(), "name", event.Name)
			// (Re)start the debounce window.
			if timerActive && !timer.Stop() {
				// Drain if it already fired.
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounce)
			timerActive = true

		case <-timer.C:
			timerActive = false
			if err := cs.ReloadAll(); err != nil {
				slog.Error("Automatic certificate reload failed", "error", err)
			} else {
				slog.Info("Certificates reloaded automatically")
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("Certificate watcher error", "error", err)
		}
	}
}

// relevantEvent reports whether a filesystem event should trigger a reload.
// It matches events whose cleaned path equals one of the watched files, and
// also Create events anywhere inside a watched directory (to catch the
// symlink swap Kubernetes performs, which may only emit events on the
// ..data symlink or a sibling name).
func (cs *CertStore) relevantEvent(event fsnotify.Event, watchedFiles, dirs map[string]struct{}) bool {
	name := event.Name
	if abs, err := filepath.Abs(name); err == nil {
		name = abs
	} else {
		name = filepath.Clean(name)
	}

	if _, ok := watchedFiles[name]; ok {
		return true
	}

	// Kubernetes atomic secret updates swap the ..data symlink; the only
	// event may be a Create on a name we don't directly watch. Treat any
	// Create inside a watched directory as relevant. A spurious reload is
	// harmless because ReloadAll is fail-safe.
	if event.Op&fsnotify.Create != 0 {
		if _, ok := dirs[filepath.Dir(name)]; ok {
			return true
		}
	}
	return false
}
