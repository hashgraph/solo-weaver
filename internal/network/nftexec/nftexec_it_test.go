// SPDX-License-Identifier: Apache-2.0

//go:build integration && linux

package nftexec

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These run the real nft binary. nftexec_test.go covers the same functions with
// a fake binary, which pins the parsing but cannot see nft's actual output or
// error wording — the two contracts that break when the nft build changes:
//
//   - `nft list tables` line format, which TableListed parses.
//   - `nft delete table` on an absent table, which DeleteTable must swallow.
//
// A fake-binary test asserts what we told it to say, so only these can catch a
// drift. They need root and run only in the VM (`task vm:test:integration`).

// scratchTable is deliberately not one of the weaver table names, so a crashed
// run can never leave the real firewall or policy tables damaged.
const scratchTable = "inet weaver-it-scratch"

// requireRoot skips unless the test can actually mutate the ruleset.
func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root to add and remove nft tables")
	}
}

// requireNft resolves the binary or skips, and returns its path.
func requireNft(t *testing.T) string {
	t.Helper()
	bin, ok := Binary()
	if !ok {
		t.Skipf("no nft binary on this host (looked for %s)", bin)
	}
	return bin
}

// addScratchTable creates the scratch table and removes it again on exit,
// whatever the test did to it.
func addScratchTable(t *testing.T, bin string) {
	t.Helper()
	require.NoError(t, exec.Command(bin, "add", "table", "inet", "weaver-it-scratch").Run(),
		"could not create the scratch table")
	t.Cleanup(func() {
		// Already-absent is fine: some tests delete it themselves.
		_ = exec.Command(bin, "delete", "table", "inet", "weaver-it-scratch").Run()
	})
}

// Test_Binary_ResolvesAnAbsolutePath pins that one of the hard-coded candidates
// is actually where nft lives on a provisioned host. If a distro moves it, every
// probe degrades to "cannot determine" and reassert stops repairing anything.
func Test_Binary_ResolvesAnAbsolutePath_Integration(t *testing.T) {
	bin, ok := Binary()

	require.True(t, ok, "nft must be found at one of %v on a provisioned host", binCandidates)
	assert.FileExists(t, bin)
}

// Test_TableExists_SeesARealTableAppearAndDisappear runs the presence probe
// against the live ruleset on both sides of a create and a delete.
func Test_TableExists_SeesARealTableAppearAndDisappear_Integration(t *testing.T) {
	requireRoot(t)
	bin := requireNft(t)
	ctx := context.Background()

	present, err := TableExists(ctx, bin, scratchTable)
	require.NoError(t, err)
	require.False(t, present, "precondition: the scratch table must not already exist")

	addScratchTable(t, bin)

	present, err = TableExists(ctx, bin, scratchTable)
	require.NoError(t, err)
	assert.True(t, present, "a table that was just created must be reported present")

	require.NoError(t, DeleteTable(ctx, bin, scratchTable))

	present, err = TableExists(ctx, bin, scratchTable)
	require.NoError(t, err)
	assert.False(t, present, "a table that was just deleted must be reported absent")
}

// Test_DeleteTable_AbsentTableIsNotAnError is the one contract a fake binary
// cannot check: DeleteTable swallows nft's "No such file or directory" so a
// repair is idempotent. If this nft build words that differently, the string
// match in DeleteTable stops matching and this fails — which is the point.
func Test_DeleteTable_AbsentTableIsNotAnError_Integration(t *testing.T) {
	requireRoot(t)
	bin := requireNft(t)

	err := DeleteTable(context.Background(), bin, "inet weaver-it-definitely-absent")

	assert.NoError(t, err,
		"deleting an absent table must be a no-op; if this fails, nft's error wording "+
			"changed and DeleteTable's string match needs updating")
}

// Test_TableListed_ParsesRealNftOutput feeds actual `nft list tables` output to
// the parser, so a change to nft's line format is caught here rather than
// silently reporting every weaver table as absent.
func Test_TableListed_ParsesRealNftOutput_Integration(t *testing.T) {
	requireRoot(t)
	bin := requireNft(t)
	addScratchTable(t, bin)

	out, err := exec.Command(bin, "list", "tables").Output()
	require.NoError(t, err)

	assert.True(t, TableListed(string(out), scratchTable),
		"the parser must find the scratch table in real output:\n%s", out)
	assert.False(t, TableListed(string(out), "inet weaver-it-absent"),
		"the parser must not match a table that is not listed:\n%s", out)
	// A family-only match would make the host firewall and workload policy
	// probes answer for each other.
	assert.False(t, TableListed(string(out), "ip weaver-it-scratch"),
		"the family must be part of the match:\n%s", out)
}

// Test_TableExists_UnknownFamilyIsAbsenceNotFailure guards the branch that
// decides whether reassert repairs: nft exits 0 for `list tables` regardless, so
// a table we never created must read as a clean absence.
func Test_TableExists_UnknownFamilyIsAbsenceNotFailure_Integration(t *testing.T) {
	requireRoot(t)
	bin := requireNft(t)

	present, err := TableExists(context.Background(), bin, "arp weaver-it-absent")

	require.NoError(t, err, "an absent table must not be a probe failure")
	assert.False(t, present)
}
