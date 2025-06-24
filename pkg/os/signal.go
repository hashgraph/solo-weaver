package os

import (
	"github.com/cockroachdb/errors"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"
)

// SignalCallback defines the OS signal callback function
type SignalCallback func(os.Signal)

// ShutdownFunc defines a cleanup function to be called on exit of the process
type ShutdownFunc func()

// SignalHandler exposes signal handling functionalities
// It allows only one callback to be registered for an OS signal.
type SignalHandler interface {
	// Register registers a callback function for the given os.Signal.
	//
	// It only allows a single callback function to be registered for an OS signal.
	// It returns error if a callback function already exists.
	//
	// If the handler has been shutdown already, it will return error for any new registrations.
	Register(sig os.Signal, cb SignalCallback) error

	// Unregister unregisters callback function for the given os.Signal.
	Unregister(sig os.Signal)

	// IsActive returns true if is active and able to process signals.
	//
	// If it is not active, attempting to register a callback will return error.
	IsActive() bool

	// HasCallback returns true if a callback is already registered for the given OS signal
	HasCallback(sig os.Signal) bool
}

type signalHandler struct {
	mu sync.Mutex

	receiver  chan os.Signal
	callbacks map[os.Signal]SignalCallback

	active bool
	stop   chan bool // used internally to stop the signal handler
	done   chan bool // used to internally signal that handler is stopped
}

func (sh *signalHandler) Register(sig os.Signal, cb SignalCallback) error {
	if sig == nil {
		return errors.New("signal cannot be nil")
	}

	if cb == nil {
		return errors.New("callback function cannot be nil")
	}

	if !sh.IsActive() {
		return errors.Newf("cannot register a callback for %s since handler is not active", sig)
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	if _, ok := sh.callbacks[sig]; ok {
		return errors.Newf("callback already exists for %s", sig)
	}

	// store the callback
	sh.callbacks[sig] = cb

	// start processing the signal
	signal.Notify(sh.receiver, sig)

	return nil
}

func (sh *signalHandler) Unregister(sig os.Signal) {
	if sig == nil {
		return
	}

	sh.mu.Lock()
	defer sh.mu.Unlock()

	delete(sh.callbacks, sig)
}

func (sh *signalHandler) shutdown() {
	if sh.IsActive() {
		close(sh.stop)

		// now wait for the done signal
		maxWait := time.Second * 10 // stop waiting after this duration
		select {
		case <-sh.done: // wait for the done signal
			sh.active = false
		case <-time.Tick(maxWait): // timeout
			log.Println("WARN Timeout - signal handler didn't stop. continuing without waiting further...")
		}
	}
}

func (sh *signalHandler) IsActive() bool {
	return sh.active
}

func (sh *signalHandler) HasCallback(sig os.Signal) bool {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	_, ok := sh.callbacks[sig]
	return ok
}

// start starts the signal processing Go routine
func (sh *signalHandler) start(wg *sync.WaitGroup) {
	if sh.active {
		return
	}

	wg.Add(1)
	go func() {
		for {
			select {
			case sig := <-sh.receiver:
				sh.invokeCallback(sig)
			case <-sh.stop:
				signal.Stop(sh.receiver)
				close(sh.done)
				return
			}
		}
	}()

	sh.active = true
	wg.Done()
}

// invokeCallback invokes the callbacks for the given signal
func (sh *signalHandler) invokeCallback(sig os.Signal) {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	if cb, ok := sh.callbacks[sig]; ok {
		cb(sig)
	}
}

// NewSignalHandler returns an instance of SignalHandler.
// Caller is expected call the returned ShutdownFunc to stop listening for all the events.
func NewSignalHandler() (SignalHandler, ShutdownFunc) {
	sh := &signalHandler{
		callbacks: map[os.Signal]SignalCallback{},
		mu:        sync.Mutex{},
		stop:      make(chan bool),
		done:      make(chan bool),
		receiver:  make(chan os.Signal, 1),
	}

	// start the signal processing go routine if it hasn't been started already
	var wg sync.WaitGroup
	sh.start(&wg)
	wg.Wait()

	return sh, func() {
		sh.shutdown()
	}
}
