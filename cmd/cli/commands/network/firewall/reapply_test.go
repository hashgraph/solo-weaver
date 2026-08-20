// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// seedRule creates the table and one fully-populated allow rule, returning
// nothing — the tests below read the result back through `show`.
func seedRule(t *testing.T) {
	t.Helper()
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))
	require.NoError(t, run(t, "create-allow-rule", "--name", "admin", "--icmp-echo"))
	require.NoError(t, run(t, "add", "--name", "admin",
		"--cidr", "203.0.113.5/32,2001:db8:5e5::/64", "--port", "22,2379-2380"))
}

func TestReapplyCmd(t *testing.T) {
	t.Run("re-applies the persisted config without changing it", func(t *testing.T) {
		nftPath, configPath := stubManager(t)
		seedRule(t)

		cfgBefore, err := os.ReadFile(configPath)
		require.NoError(t, err)
		nftBefore, err := os.ReadFile(nftPath)
		require.NoError(t, err)

		require.NoError(t, run(t, "reapply"))

		cfgAfter, err := os.ReadFile(configPath)
		require.NoError(t, err)
		nftAfter, err := os.ReadFile(nftPath)
		require.NoError(t, err)
		require.Equal(t, string(cfgBefore), string(cfgAfter))
		require.Equal(t, string(nftBefore), string(nftAfter))
	})

	t.Run("errors when nothing is persisted", func(t *testing.T) {
		stubManager(t)

		err := run(t, "reapply")

		// Applying a default table here would mean a default-drop policy with an
		// empty management allowlist.
		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	// The whole point of separating reapply from create: re-asserting the table
	// states no intent, so a later `block node reconfigure` must see exactly the
	// decision it would have seen without the reapply (issue #1003's record).
	t.Run("records no enable decision", func(t *testing.T) {
		stubManager(t)
		fake := stubStateManager(t)
		require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))
		fake.flushed = nil

		require.NoError(t, run(t, "reapply"))

		require.Nil(t, fake.flushed, "reapply must not touch the persisted host-firewall decision")
	})

	t.Run("takes no input flag", func(t *testing.T) {
		stubManager(t)

		// Given a path this would just be `create --from-file` under a second
		// name, and the two would disagree about what the persisted state is.
		require.Error(t, run(t, "reapply", "--from-file", "/tmp/whatever.yaml"))
	})
}

func TestShowCmd_OutputCommands(t *testing.T) {
	t.Run("emits declare then a single populate", func(t *testing.T) {
		stubManager(t)
		seedRule(t)

		out, err := runOut(t, "show", "--name", "admin", "--output", "commands")
		require.NoError(t, err)

		lines := nonEmptyLines(out)
		require.Len(t, lines, 2, "one declare and one add, not one add per value")
		// The binary name is read from the running command, not hardcoded, so the
		// test root's name is what shows up here.
		require.Equal(t, "test network firewall create-allow-rule --name admin --icmp-echo", lines[0])
		// The address list comes out in stored order, which Rule.AddCIDRs keeps
		// sorted (rule.go:230) — the export projects what is persisted, not what
		// was typed, which is exactly what makes it faithful.
		require.Equal(t,
			"test network firewall add --name admin --cidr 2001:db8:5e5::/64,203.0.113.5/32 --port 22,2379-2380",
			lines[1])
	})

	t.Run("projects every field losslessly", func(t *testing.T) {
		stubManager(t)
		require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))
		require.NoError(t, run(t, "create-allow-rule", "--name", "cilium_vxlan", "--proto", "udp"))
		require.NoError(t, run(t, "add", "--name", "cilium_vxlan", "--cidr", "10.0.0.0/24", "--port", "8472"))

		out, err := runOut(t, "show", "--name", "cilium_vxlan", "--output", "commands")
		require.NoError(t, err)

		require.Contains(t, out, "--proto udp")
		require.Contains(t, out, "--port 8472")
		require.NotContains(t, out, "--icmp-echo")
	})

	t.Run("omits proto when the rule is on the default", func(t *testing.T) {
		stubManager(t)
		require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))
		require.NoError(t, run(t, "create-allow-rule", "--name", "plain"))
		require.NoError(t, run(t, "add", "--name", "plain", "--cidr", "10.0.0.0/24", "--port", "443"))

		out, err := runOut(t, "show", "--name", "plain", "--output", "commands")
		require.NoError(t, err)

		// A defaulted rule must emit the same declare line that produced it.
		require.NotContains(t, out, "--proto")
	})

	t.Run("a declared but unpopulated rule emits only the declare", func(t *testing.T) {
		stubManager(t)
		require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))
		require.NoError(t, run(t, "create-allow-rule", "--name", "empty"))

		out, err := runOut(t, "show", "--name", "empty", "--output", "commands")
		require.NoError(t, err)

		// An `add` with neither list would fail, so there is nothing to emit.
		require.Equal(t, []string{"test network firewall create-allow-rule --name empty"}, nonEmptyLines(out))
	})

	t.Run("requires --name", func(t *testing.T) {
		stubManager(t)
		seedRule(t)

		_, err := runOut(t, "show", "--output", "commands")

		require.Error(t, err)
		require.Contains(t, err.Error(), "requires --name")
		// The whole-table answer already exists and is exact.
		require.Contains(t, err.Error(), "yaml")
	})

	t.Run("rejects a reserved block", func(t *testing.T) {
		stubManager(t)
		seedRule(t)

		_, err := runOut(t, "show", "--name", "mgmt", "--output", "commands")

		require.Error(t, err)
		require.Contains(t, err.Error(), "reserved block")
		require.Contains(t, err.Error(), "yaml")
	})

	t.Run("rejects an unknown output format", func(t *testing.T) {
		stubManager(t)
		seedRule(t)

		_, err := runOut(t, "show", "--name", "admin", "--output", "bogus")

		require.Error(t, err)
		require.Contains(t, err.Error(), "commands")
	})
}

// TestShowCmd_OutputCommandsRoundTrip is the lossless-projection acceptance: the
// emitted subsequence, replayed on a host that has a table but not this rule,
// reproduces the rule exactly — and disturbs no other rule.
func TestShowCmd_OutputCommandsRoundTrip(t *testing.T) {
	stubManager(t)
	seedRule(t)

	emitted, err := runOut(t, "show", "--name", "admin", "--output", "commands")
	require.NoError(t, err)
	want, err := runOut(t, "show", "--name", "admin", "--output", "yaml")
	require.NoError(t, err)

	// Fresh host: same reserved blocks, plus an unrelated rule that must survive.
	stubManager(t)
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))
	require.NoError(t, run(t, "create-allow-rule", "--name", "bystander"))
	require.NoError(t, run(t, "add", "--name", "bystander", "--cidr", "10.9.0.0/24", "--port", "9000"))
	bystanderBefore, err := runOut(t, "show", "--name", "bystander", "--output", "yaml")
	require.NoError(t, err)

	for _, line := range nonEmptyLines(emitted) {
		args := strings.Fields(strings.TrimPrefix(line, "test network firewall "))
		require.NoError(t, run(t, args...), "replaying %q", line)
	}

	got, err := runOut(t, "show", "--name", "admin", "--output", "yaml")
	require.NoError(t, err)
	require.Equal(t, want, got, "the replayed rule must match the exported one field for field")

	bystanderAfter, err := runOut(t, "show", "--name", "bystander", "--output", "yaml")
	require.NoError(t, err)
	require.Equal(t, bystanderBefore, bystanderAfter, "replaying one rule must not disturb another")
}

// nonEmptyLines splits captured output into its non-blank lines.
func nonEmptyLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
