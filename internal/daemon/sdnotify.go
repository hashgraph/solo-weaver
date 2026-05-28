// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"net"
	"os"
)

const (
	sdReady    = "READY=1"
	sdStopping = "STOPPING=1"
)

// sdNotify sends a state string to systemd via the NOTIFY_SOCKET Unix datagram
// socket. It is a no-op when NOTIFY_SOCKET is not set (manual runs, tests).
// Errors are intentionally swallowed — a failed notify must never crash the daemon.
func sdNotify(state string) error {
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
