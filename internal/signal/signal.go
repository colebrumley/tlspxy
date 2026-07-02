package signal

import (
	"fmt"
	"log/slog"
	"os"
	ossignal "os/signal"
	"sync"
	"syscall"
)

// SigHandlerMux watches for kill signals and then executes
// handlers.
type SigHandlerMux struct {
	mu sync.RWMutex
	do map[os.Signal][]func()
}

// New creates a new SigHandlerMux ready for use.
func New() *SigHandlerMux {
	return &SigHandlerMux{
		do: map[os.Signal][]func(){},
	}
}

// AddHandler registers a function to one or more signals
func (shm *SigHandlerMux) AddHandler(fn func(), sigs ...os.Signal) {
	shm.mu.Lock()
	defer shm.mu.Unlock()
	for _, sig := range sigs {
		shm.do[sig] = append(shm.do[sig], fn)
	}
}

// WatchForSignals monitors for os kill signals and runs arbitrary
// cleanup callbacks before exiting.
func (shm *SigHandlerMux) WatchForSignals() {
	signals := make(chan os.Signal, 1)
	ossignal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	for s := range signals {
		slog.Warn(fmt.Sprintf("Trapped signal: %v", s))
		shm.mu.RLock()
		handlers := shm.do[s]
		shm.mu.RUnlock()
		wg := sync.WaitGroup{}
		for _, fn := range handlers {
			wg.Add(1)
			go func(f func()) {
				f()
				wg.Done()
			}(fn)
		}
		wg.Wait()
	}
}
