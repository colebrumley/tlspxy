package main

import (
	"os"
	"os/signal"
	"sync"

	log "github.com/Sirupsen/logrus"
)

// SigHandlerMux watches for kill signals and then executes
// handlers.
type SigHandlerMux struct {
	do map[os.Signal][]func()
}

// AddHandler registers a function to one or more signals
func (shm *SigHandlerMux) AddHandler(fn func(), sigs ...os.Signal) {
	for _, sig := range sigs {
		handlers := append(shm.do[sig], fn)
		shm.do[sig] = handlers
	}
}

// WatchForSignals monitors for os kill signals and runs arbitrary
// cleanup callbacks before exiting.
func (shm *SigHandlerMux) WatchForSignals() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill)
	for {
		select {
		case s := <-signals:
			log.Warningln("Trapped signal:", s)
			wg := sync.WaitGroup{}
			for _, fn := range shm.do[s] {
				wg.Add(1)
				go func(f func()) {
					f()
					wg.Done()
				}(fn)
			}
			wg.Wait()
			os.Exit(1)
		}
	}
}
