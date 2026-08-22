// SPDX-License-Identifier: Apache-2.0

//go:build integration

package firewall

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// stockNftablesConf mirrors Debian's stock /etc/nftables.conf: a `flush ruleset`
// then the manager's own table. Surviving this flush is what #982 is about.
const stockNftablesConf = `#!/usr/sbin/nft -f
flush ruleset

table inet filter {
	chain input {
		type filter hook input priority 0; policy accept;
	}
}
`

// ipBinCandidates mirrors nftBinCandidates: absolute paths only, never a bare
// "ip" off PATH (see docs/dev/security-model.md).
var ipBinCandidates = []string{"/usr/sbin/ip", "/sbin/ip", "/usr/bin/ip", "/bin/ip"}

// resolveBin returns the first candidate that exists, or skips the test.
func resolveBin(t *testing.T, what string, candidates []string) string {
	t.Helper()
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	t.Skipf("%s not found in %v", what, candidates)
	return ""
}

// newNetns creates a throwaway network namespace and deletes it afterwards.
// Rulesets are per-netns, so the `flush ruleset` cannot touch the host's tables.
func newNetns(t *testing.T, ipBin, name string) string {
	t.Helper()
	ctx := context.Background()

	// A namespace left behind by a killed run would make `add` fail.
	_ = exec.CommandContext(ctx, ipBin, "netns", "delete", name).Run()

	out, err := exec.CommandContext(ctx, ipBin, "netns", "add", name).CombinedOutput()
	require.NoError(t, err, "could not create network namespace %s: %s", name, out)

	t.Cleanup(func() {
		_ = exec.CommandContext(context.Background(), ipBin, "netns", "delete", name).Run()
	})
	return name
}

// loadInNetns applies an nft document inside the namespace, the way the loader
// unit does at boot.
func loadInNetns(t *testing.T, ipBin, nftBin, ns, path string) {
	t.Helper()
	out, err := exec.CommandContext(context.Background(),
		ipBin, "netns", "exec", ns, nftBin, "-f", path).CombinedOutput()
	require.NoError(t, err, "nft -f %s failed in netns %s: %s", path, ns, out)
}

// tablesInNetns returns `nft list tables` for the namespace.
func tablesInNetns(t *testing.T, ipBin, nftBin, ns string) string {
	t.Helper()
	out, err := exec.CommandContext(context.Background(),
		ipBin, "netns", "exec", ns, nftBin, "list", "tables").CombinedOutput()
	require.NoError(t, err, "nft list tables failed in netns %s: %s", ns, out)
	return string(out)
}

// writeDoc writes an nft document to a temp file and returns its path.
func writeDoc(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// Test_NetworkNftBootOrder_Integration is the load-order half of #982: with the
// real nft binary, loading after the manager keeps the weaver table.
//
// The second subtest is the negative control: loading before the manager must
// lose the table. Without it, the first subtest would also pass against a
// `flush ruleset` that does nothing, proving nothing about the ordering.
func Test_NetworkNftBootOrder_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	ipBin := resolveBin(t, "ip", ipBinCandidates)
	nftBin := resolveBin(t, "nft", nftBinCandidates)

	weaverDoc, err := sampleTable().Render()
	require.NoError(t, err)

	dir := t.TempDir()
	weaverPath := writeDoc(t, dir, "network-weaver-host-firewall.nft", weaverDoc)
	managerPath := writeDoc(t, dir, "nftables.conf", stockNftablesConf)

	t.Run("weaver loaded after the manager survives its flush", func(t *testing.T) {
		ns := newNetns(t, ipBin, "weaver-it-after")

		loadInNetns(t, ipBin, nftBin, ns, managerPath)
		loadInNetns(t, ipBin, nftBin, ns, weaverPath)

		tables := tablesInNetns(t, ipBin, nftBin, ns)
		require.Contains(t, tables, TableName,
			"the weaver table must be present when the loader runs last")
		require.Contains(t, tables, "inet filter",
			"loading last must not cost the manager its own table — the weaver document does not flush")
	})

	t.Run("weaver loaded before the manager is wiped by its flush", func(t *testing.T) {
		ns := newNetns(t, ipBin, "weaver-it-before")

		loadInNetns(t, ipBin, nftBin, ns, weaverPath)
		require.Contains(t, tablesInNetns(t, ipBin, nftBin, ns), TableName,
			"precondition: the weaver document must load cleanly on its own")

		loadInNetns(t, ipBin, nftBin, ns, managerPath)

		require.NotContains(t, tablesInNetns(t, ipBin, nftBin, ns), TableName,
			"the manager's `flush ruleset` must wipe a weaver table loaded before it — "+
				"if this ever holds the table, the flush is inert and the ordering test above proves nothing")
	})
}
