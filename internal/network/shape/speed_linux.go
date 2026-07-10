// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"fmt"
	"strings"
)

// ReadLinkSpeedMbit reads the link speed of nic from the kernel sysfs entry
// /sys/class/net/<nic>/speed and returns it in Mbit/s.
//
// Returns (0, false) when the speed is unavailable: the file is missing (virtual
// NIC, tunnel), the value is non-positive (kernel reports -1 for unknown/down
// links), or the content is non-numeric.  Callers treat false as "no hint
// available" and must never block an install on this.
func ReadLinkSpeedMbit(nic string) (int, bool) {
	if strings.ContainsRune(nic, '/') {
		return 0, false
	}
	return readLinkSpeedMbitFrom(fmt.Sprintf("/sys/class/net/%s/speed", nic))
}
