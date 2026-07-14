// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// readLinkSpeedMbitFrom reads a /sys/class/net/<nic>/speed-style file and
// returns its Mbit/s value. Exported via ReadLinkSpeedMbit on Linux; available
// here for cross-platform unit tests using temp files.
func readLinkSpeedMbitFrom(path string) (int, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// FormatSpeedHint converts a Mbit/s value into a tc-style bandwidth string
// suitable for operator-facing prompts (e.g. 1000 → "1gbit", 10000 → "10gbit",
// 100 → "100mbit").
func FormatSpeedHint(mbit int) string {
	if mbit >= 1000 && mbit%1000 == 0 {
		return fmt.Sprintf("%dgbit", mbit/1000)
	}
	return fmt.Sprintf("%dmbit", mbit)
}

// ParseSpeedMbit parses a tc-style bandwidth string (e.g. "1gbit", "100mbit")
// into Mbit/s. Returns (0, false) for empty input or unrecognised formats.
func ParseSpeedMbit(s string) (int, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, false
	}
	if strings.HasSuffix(s, "gbit") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "gbit"))
		if err != nil || n <= 0 {
			return 0, false
		}
		return n * 1000, true
	}
	if strings.HasSuffix(s, "mbit") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "mbit"))
		if err != nil || n <= 0 {
			return 0, false
		}
		return n, true
	}
	return 0, false
}
