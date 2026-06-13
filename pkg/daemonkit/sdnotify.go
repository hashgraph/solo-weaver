// SPDX-License-Identifier: Apache-2.0

package daemonkit

import (
	"net"
	"os"
)

const (
	sdReady    = "READY=1"
	sdStopping = "STOPPING=1"
)

// notify sends a state string to systemd via the NOTIFY_SOCKET Unix datagram
// socket. It is a no-op when NOTIFY_SOCKET is not set (manual runs, tests).
// A failed notify must never crash the daemon — callers typically log and
// continue.
func notify(state string) error {
	sock := os.Getenv("NOTIFY_SOCKET")
	if sock == "" {
		return nil
	}

	conn, err := net.Dial("unixgram", sock)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write([]byte(state))
	return err
}

// NotifyReady sends READY=1 to systemd, signalling that the daemon has finished
// startup and its socket is serving. No-op when NOTIFY_SOCKET is unset.
func NotifyReady() error { return notify(sdReady) }

// NotifyStopping sends STOPPING=1 to systemd, signalling that the daemon has
// begun a graceful shutdown. No-op when NOTIFY_SOCKET is unset.
func NotifyStopping() error { return notify(sdStopping) }
