// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/joomcode/errorx"
)

// detectEgressInterfaceFrom parses a file in /proc/net/route format and
// returns the name of the interface carrying the default route (destination
// 0.0.0.0 with RTF_UP|RTF_GATEWAY set). Exported via DetectEgressInterface
// on Linux; available here for cross-platform unit tests using temp files.
func detectEgressInterfaceFrom(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", errorx.ExternalError.Wrap(err, "failed to open %s", path)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Scan() // skip header line

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		iface := fields[0]
		dest := fields[1]
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil {
			continue
		}
		const (
			rtfUp      = 0x1
			rtfGateway = 0x2
		)
		if dest == "00000000" && flags&rtfGateway != 0 && flags&rtfUp != 0 {
			return iface, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", errorx.ExternalError.Wrap(err, "failed to read %s", path)
	}
	return "", errorx.IllegalState.New(
		"no default route found in %s; specify --egress-interface explicitly", path)
}
