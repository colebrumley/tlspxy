package signal

import (
	"os"
	"sync"
	"syscall"
	"testing"
)

func TestAddHandler(t *testing.T) {
	shm := New()

	called := false
	handler := func() { called = true }

	shm.AddHandler(handler, os.Interrupt)

	if len(shm.do[os.Interrupt]) != 1 {
		t.Errorf("expected 1 handler for Interrupt, got %d", len(shm.do[os.Interrupt]))
	}

	// Execute the handler to verify it's the right one
	shm.do[os.Interrupt][0]()
	if !called {
		t.Error("handler was not called")
	}
}

func TestAddHandler_MultipleSignals(t *testing.T) {
	shm := New()

	handler := func() {}
	shm.AddHandler(handler, os.Interrupt, syscall.SIGTERM)

	if len(shm.do[os.Interrupt]) != 1 {
		t.Errorf("expected 1 handler for Interrupt, got %d", len(shm.do[os.Interrupt]))
	}
	if len(shm.do[syscall.SIGTERM]) != 1 {
		t.Errorf("expected 1 handler for SIGTERM, got %d", len(shm.do[syscall.SIGTERM]))
	}
}

func TestAddHandler_MultipleHandlers(t *testing.T) {
	shm := New()

	count := 0
	shm.AddHandler(func() { count++ }, os.Interrupt)
	shm.AddHandler(func() { count += 10 }, os.Interrupt)

	if len(shm.do[os.Interrupt]) != 2 {
		t.Errorf("expected 2 handlers for Interrupt, got %d", len(shm.do[os.Interrupt]))
	}

	// Execute both handlers
	for _, fn := range shm.do[os.Interrupt] {
		fn()
	}
	if count != 11 {
		t.Errorf("count = %d, want 11", count)
	}
}

func TestAddHandler_SIGTERMRegistered(t *testing.T) {
	shm := New()

	executed := false
	shm.AddHandler(func() { executed = true }, syscall.SIGTERM)

	if handlers, ok := shm.do[syscall.SIGTERM]; !ok {
		t.Error("SIGTERM not registered in handler map")
	} else if len(handlers) != 1 {
		t.Errorf("expected 1 handler for SIGTERM, got %d", len(handlers))
	}

	// Verify the handler actually runs
	shm.do[syscall.SIGTERM][0]()
	if !executed {
		t.Error("SIGTERM handler was not executed")
	}
}

func TestSigHandlerMux_ConcurrentAdd(t *testing.T) {
	shm := New()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			shm.AddHandler(func() {}, os.Interrupt, syscall.SIGTERM)
		}()
	}
	wg.Wait()

	shm.mu.RLock()
	intCount := len(shm.do[os.Interrupt])
	termCount := len(shm.do[syscall.SIGTERM])
	shm.mu.RUnlock()

	if intCount != 100 {
		t.Errorf("expected 100 handlers for Interrupt, got %d", intCount)
	}
	if termCount != 100 {
		t.Errorf("expected 100 handlers for SIGTERM, got %d", termCount)
	}
}
