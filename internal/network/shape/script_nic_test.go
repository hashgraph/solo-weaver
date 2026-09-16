// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeNICScript writes content to a temp file and returns its path.
func writeNICScript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "solo-provisioner-bandwidth-shaper.sh")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
	return path
}

func TestEgressNICFromScript_AbsentScriptIsNotAnError(t *testing.T) {
	// An absent script is the "egress shaping was never provisioned here" signal,
	// which callers must be able to distinguish from a corrupt one.
	nic, ok, err := egressNICFromScript(filepath.Join(t.TempDir(), "nope.sh"))

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, nic)
}

func TestEgressNICFromScript_ReadsTheRenderedNIC(t *testing.T) {
	path := writeNICScript(t, "#!/bin/sh\nset -e\n\nNIC=\"eth0\"\n\n"+
		"tc qdisc del dev \"$NIC\" root\ntc qdisc add dev \"$NIC\" root handle 1: htb default 30\n")

	nic, ok, err := egressNICFromScript(path)

	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "eth0", nic)
}

func TestEgressNICFromScript_ScriptThatBuildsNoRootIsUnshaped(t *testing.T) {
	// Only a live `tc qdisc add … root` line makes the host shaped, or a hand-edit
	// could turn a disabled plane into an expected-but-missing one to rebuild.
	path := writeNICScript(t, "#!/bin/sh\nNIC=\"eth0\"\n"+
		"# tc qdisc add dev \"$NIC\" root handle 1: htb default 30\n"+
		"tc qdisc del dev \"$NIC\" root 2>/dev/null || true\n")

	nic, ok, err := egressNICFromScript(path)

	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, "eth0", nic, "the pinned NIC is still reported so the caller can say which")
}

// TestEgressNICFromScript_RoundTripsTheRealRender parses back what the
// production template emits, so a NIC-line change breaks this test, not reassert.
func TestEgressNICFromScript_RoundTripsTheRealRender(t *testing.T) {
	for _, nic := range []string{"eth0", "bond0", "eno1.100", "br-ex"} {
		t.Run(nic, func(t *testing.T) {
			rendered, err := renderTcEgressScript(nic)
			require.NoError(t, err)

			got, ok, err := egressNICFromScript(writeNICScript(t, rendered))
			require.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, nic, got)
		})
	}
}

// The teardown render pins a NIC but builds nothing, so it must read as "not
// provisioned" rather than absent hierarchy to rebuild — though the NIC still comes back.
func TestEgressNICFromScript_RoundTripsTheUnshapeRender(t *testing.T) {
	rendered, err := renderTcEgressUnshapeScript("eth1")
	require.NoError(t, err)

	nic, ok, err := egressNICFromScript(writeNICScript(t, rendered))

	require.NoError(t, err)
	assert.False(t, ok, "the teardown render must not read as shaped")
	assert.Equal(t, "eth1", nic)
}

func TestEgressNICFromScript_ScriptWithoutNICLineIsAnError(t *testing.T) {
	// A script that exists but declares nothing is corrupt, not "unprovisioned":
	// treating it as absent would hide a real outage from the presence check.
	path := writeNICScript(t, "#!/bin/sh\nset -e\n# someone truncated this\n")

	_, ok, err := egressNICFromScript(path)

	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "declares no NIC")
}

func TestEgressNICFromScript_RejectsAnInvalidNICName(t *testing.T) {
	// The value reaches a privileged tc argv, so a hand-edited script must not
	// widen what the render path validates against.
	for name, line := range map[string]string{
		"shell metacharacters": "NIC=\"eth0; rm -rf /\"\n",
		"too long for IFNAMSIZ": "NIC=\"" +
			"0123456789abcdefg\"\n",
		"empty": "NIC=\"\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, ok, err := egressNICFromScript(writeNICScript(t, "#!/bin/sh\n"+line))

			require.Error(t, err)
			assert.False(t, ok)
			assert.Contains(t, err.Error(), "invalid NIC name")
		})
	}
}

func TestParseScriptNICLine(t *testing.T) {
	for name, tc := range map[string]struct {
		line string
		want string
		ok   bool
	}{
		"plain":            {`NIC="eth0"`, "eth0", true},
		"leading space":    {`  NIC="eth0"`, "eth0", true},
		"trailing comment": {`NIC="eth0" # set at render time`, "eth0", true},
		"other assignment": {`WAIT_SECS="30"`, "", false},
		"usage of the var": {`tc qdisc del dev "$NIC" root`, "", false},
		"unterminated":     {`NIC="eth0`, "", false},
		"comment line":     {`# NIC="eth0"`, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := parseScriptNICLine(tc.line)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}
