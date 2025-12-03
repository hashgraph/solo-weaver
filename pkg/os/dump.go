// SPDX-License-Identifier: Apache-2.0

package os

import (
	"log"
	"runtime"
)

// GoDump dumps output of goroutine dump.
//
// In order to have this in the output, one need to send a signal (e.g. SIGQUIT) signal to the process using
// kill: `kill -SIGQUIT <pid>` and register a callback for that OS signal(i.e. SIGQUIT) with the SignalHandler.
//
// Example:
// assuming `handler` is an instance of SignalHandler and we are adding SIGQUIT handler for a systemd daemon.
//
//	handler.Register(syscall.SIGQUIT, func(os.Signal) {
//			nmtos.GoDump()
//			logger.Debug("Received SIGQUIT. " +
//				"Check Go routine dump in logs using 'sudo journalctl --unit <daemon-name> --follow'." +
//				"Continuing daemon operation as before...")
//		})
func GoDump() {
	buf := make([]byte, 1<<20)
	stackLen := runtime.Stack(buf, true)
	log.Printf("===== goroutine dump start =====\n%s\n===== goroutine dump end =====\n", buf[:stackLen])
}
