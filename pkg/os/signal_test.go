/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 *
 *
 */

package os

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

type SignalHandlerTestSuite struct {
	suite.Suite
}

func (sht *SignalHandlerTestSuite) testCallback(t *testing.T, ticker *time.Ticker, expected int, received chan os.Signal) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		counter := 0
		for {
			select {
			case <-received:
				counter++
				if counter == expected {
					t.Logf("Found callbacks %d/%d", counter, expected)
					wg.Done()
					return
				}
			case <-ticker.C:
				if counter != expected {
					t.Logf("Found callbacks %d/%d", counter, expected)
					t.Fail()
				}

				wg.Done()
				return
			}
		}
	}()
	wg.Wait()
}

func (sht *SignalHandlerTestSuite) TestRegister() {
	t := sht.T()
	req := require.New(t)

	received := make(chan os.Signal)
	sig := syscall.SIGINT
	cb := func(s os.Signal) {
		t.Logf("Received signal: %v", s)
		received <- s
	}

	sh, shutdown := NewSignalHandler()
	defer shutdown()

	req.True(sh.(*signalHandler).active)

	// ensure error are checked during init
	err := sh.Register(nil, cb)
	req.Error(err)

	err = sh.Register(sig, nil)
	req.Error(err)

	// register two callback for the same signal
	err = sh.Register(sig, cb)
	req.NoError(err)

	// Send signal to the process
	syscall.Kill(syscall.Getpid(), sig)

	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()
	sht.testCallback(t, ticker, 1, received)
}

func (sht *SignalHandlerTestSuite) TestRegister_Two_Callbacks_For_Same_Signal() {
	t := sht.T()
	req := require.New(t)

	received := make(chan os.Signal)
	sig := syscall.SIGINT

	sh, shutdown := NewSignalHandler()
	defer shutdown()

	// register two callback for the same signal
	err := sh.Register(sig, func(s os.Signal) {
		t.Logf("Received signal in callback-1: %v", s)
		received <- s
	})
	req.NoError(err)

	err = sh.Register(sig, func(s os.Signal) {
		t.Logf("Received signal in callback-2: %v", s)
		received <- s
	})
	req.Error(err)
	req.Contains(err.Error(), "callback already exists for")

	// Send signal to the process
	syscall.Kill(syscall.Getpid(), sig)

	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()
	sht.testCallback(t, ticker, 1, received)
}

func (sht *SignalHandlerTestSuite) TestRegister_Two_Callbacks_For_Different_Signal() {
	t := sht.T()
	req := require.New(t)

	received := make(chan os.Signal)

	sh, shutdown := NewSignalHandler()
	defer shutdown()

	// register two callback for the same signal
	err := sh.Register(syscall.SIGQUIT, func(s os.Signal) {
		t.Logf("Received signal in callback-1: %v", s)
		received <- s
	})
	req.NoError(err)

	err = sh.Register(syscall.SIGHUP, func(s os.Signal) {
		t.Logf("Received signal in callback-2: %v", s)
		received <- s
	})
	req.NoError(err)

	// Send signal to the process
	syscall.Kill(syscall.Getpid(), syscall.SIGQUIT)
	syscall.Kill(syscall.Getpid(), syscall.SIGHUP)

	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()
	sht.testCallback(t, ticker, 2, received)
}

func (sht *SignalHandlerTestSuite) TestUnregister() {
	t := sht.T()
	req := require.New(t)

	received := make(chan os.Signal)
	sig := syscall.SIGINT

	sh, shutdown := NewSignalHandler()
	defer shutdown()

	err := sh.Register(sig, func(s os.Signal) {
		t.Logf("Received signal: %v", s)
		received <- s
	})
	req.NoError(err)

	req.Equal(1, len(sh.(*signalHandler).callbacks))
	sh.Unregister(nil) // no effect
	req.Equal(1, len(sh.(*signalHandler).callbacks))

	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()

	// Send signal to the process
	syscall.Kill(syscall.Getpid(), sig)
	sht.testCallback(t, ticker, 1, received)

	req.Equal(1, len(sh.(*signalHandler).callbacks))
	sh.Unregister(sig) // unregister
	req.Equal(0, len(sh.(*signalHandler).callbacks))

	// Send signal to the process again, but it won't be processed
	syscall.Kill(syscall.Getpid(), sig)
	sht.testCallback(t, ticker, 0, received)
}

func (sht *SignalHandlerTestSuite) TestRegisterErrorAfterStop() {
	t := sht.T()
	req := require.New(t)

	received := make(chan os.Signal)
	sig := syscall.SIGINT

	sh, shutdown := NewSignalHandler()
	//defer shutdown() // we'll call it later

	req.True(sh.IsActive())

	err := sh.Register(sig, func(s os.Signal) {
		t.Logf("Received signal: %v", s)
		received <- s
	})
	req.NoError(err)

	ticker := time.NewTicker(time.Millisecond * 100)
	defer ticker.Stop()
	sht.testCallback(t, ticker, 0, received)

	shutdown()
	req.Falsef(sh.IsActive(), "signal handler should've stopped")

	err = sh.Register(sig, func(s os.Signal) {
		t.Logf("Received signal: %v", s)
	})
	req.Error(err)
	req.Contains(err.Error(), fmt.Sprintf("cannot register a callback for %s since handler is not active", sig))
}

func (sht *SignalHandlerTestSuite) TestHasCallback() {
	t := sht.T()
	req := require.New(t)

	received := make(chan os.Signal)
	sig := syscall.SIGINT

	sh, shutdown := NewSignalHandler()
	defer shutdown()

	req.True(sh.IsActive())
	req.False(sh.HasCallback(sig))

	err := sh.Register(sig, func(s os.Signal) {
		t.Logf("Received signal: %v", s)
		received <- s
	})
	req.NoError(err)
	req.True(sh.HasCallback(sig))
}

func TestSignalHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(SignalHandlerTestSuite))
}
