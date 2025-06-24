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
