// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"bufio"
	"os"
	"strings"

	"github.com/joomcode/errorx"
)

// scriptNICPrefix starts the boot script's `NIC="<name>"` line.
const scriptNICPrefix = `NIC="`

// scriptRootAddMarker is on the line that builds the HTB root. The teardown
// render has no such line.
const scriptRootAddMarker = `qdisc add dev "$NIC" root`

// EgressNICFromScript reads the NIC and whether shaping is provisioned from the
// boot script — a teardown render still returns the NIC but with shaped=false.
// An absent script returns ("", false, nil); one without a NIC line is an error.
func EgressNICFromScript() (nic string, shaped bool, err error) {
	return egressNICFromScript(TcEgressScriptPath)
}

// egressNICFromScript is EgressNICFromScript with the path injected.
func egressNICFromScript(path string) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, errorx.ExternalError.Wrap(err, "failed to read %s", path)
	}
	defer func() { _ = f.Close() }()

	var nic string
	shaped := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, scriptRootAddMarker) {
			shaped = true
			continue
		}
		if nic != "" {
			continue
		}
		n, ok := parseScriptNICLine(line)
		if !ok {
			continue
		}
		// The value reaches a privileged tc argv, so validate it like the render path.
		if !nicNameRe.MatchString(n) {
			return "", false, errorx.IllegalFormat.New(
				"%s declares an invalid NIC name %q: must match %s", path, n, nicNameRe.String())
		}
		nic = n
	}
	if err := scanner.Err(); err != nil {
		return "", false, errorx.ExternalError.Wrap(err, "failed to scan %s", path)
	}
	if nic == "" {
		return "", false, errorx.IllegalFormat.New(
			"%s declares no NIC; re-render it with `network shape create --device egress`", path)
	}
	return nic, shaped, nil
}

// parseScriptNICLine extracts the name from a `NIC="<name>"` line.
func parseScriptNICLine(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, scriptNICPrefix) {
		return "", false
	}
	nic, _, ok := strings.Cut(line[len(scriptNICPrefix):], `"`)
	if !ok {
		return "", false
	}
	return nic, true
}
