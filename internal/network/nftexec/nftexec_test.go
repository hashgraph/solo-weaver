// SPDX-License-Identifier: Apache-2.0

package nftexec

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/network/nftexec/nfttest"
)

const testTable = "inet weaver-host-firewall"

func TestTableExists_TableListed(t *testing.T) {
	out := "table ip nat\ntable " + testTable + "\ntable inet weaver-workload-policy\n"
	present, err := TableExists(context.Background(), nfttest.FakeNft(t, out, 0), testTable)
	require.NoError(t, err)
	assert.True(t, present)
}

func TestTableExists_TableNotListed(t *testing.T) {
	// A definite "no" — the state that must stay repairable.
	bin := nfttest.FakeNft(t, "table ip nat\ntable inet weaver-workload-policy\n", 0)
	present, err := TableExists(context.Background(), bin, testTable)
	require.NoError(t, err)
	assert.False(t, present)
}

func TestTableExists_NoTablesAtAllIsAbsenceNotFailure(t *testing.T) {
	// No tables at all: nothing printed, exit 0. Still an absence, not a failure.
	present, err := TableExists(context.Background(), nfttest.FakeNft(t, "", 0), testTable)
	require.NoError(t, err)
	assert.False(t, present)
}

func TestTableExists_NonZeroExitIsAnErrorNotAbsence(t *testing.T) {
	// The invariant: nft could not answer, so say so rather than "table gone".
	present, err := TableExists(context.Background(), nfttest.FakeNft(t, "", 1), testTable)
	require.Error(t, err)
	assert.False(t, present)
	assert.Contains(t, err.Error(), "cannot be determined")
}

func TestTableExists_UnrunnableBinaryIsAnError(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "absent-nft")
	present, err := TableExists(context.Background(), bin, testTable)
	require.Error(t, err)
	assert.False(t, present)
}

func TestTableListed_MatchesFamilyAndNameOnly(t *testing.T) {
	// A prefix match or a different role must not read as a hit.
	assert.False(t, TableListed("table "+testTable+"-staging\n", testTable))
	assert.False(t, TableListed("chain "+testTable+"\n", testTable))
	assert.False(t, TableListed("table ip weaver-host-firewall\n", testTable))
	assert.False(t, TableListed("", testTable))
	assert.True(t, TableListed("  table "+testTable+"  \n", testTable))
	assert.True(t, TableListed("table "+testTable, testTable))
	// Trailing content tolerated: a handle comment must not read as absence.
	assert.True(t, TableListed("table "+testTable+" # handle 7\n", testTable))
}

func TestBinary_OneListAnswersForBothTables(t *testing.T) {
	// Firewall and policy used to keep separate candidate lists; one shared
	// resolver makes that asymmetry unrepresentable.
	bin, found := Binary()
	assert.Contains(t, binCandidates, bin, "must return a candidate, found or not")
	if !found {
		assert.Equal(t, binCandidates[0], bin, "with none found, fall back to the first")
	}
}

func TestDeleteTable_Succeeds(t *testing.T) {
	require.NoError(t, DeleteTable(context.Background(), nfttest.FakeNft(t, "", 0), testTable))
}

func TestDeleteTable_AbsentTableIsNotAnError(t *testing.T) {
	bin := nfttest.FakeNftStderr(t, "", "Error: No such file or directory\ndelete table "+testTable+"\n", 1)
	require.NoError(t, DeleteTable(context.Background(), bin, testTable))
}

func TestDeleteTable_OtherFailurePropagates(t *testing.T) {
	// Only "not there" is tolerated: a permission error must surface, or a
	// caller would read a live table as deleted.
	bin := nfttest.FakeNftStderr(t, "", "Error: Operation not permitted\n", 1)
	err := DeleteTable(context.Background(), bin, testTable)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Operation not permitted")
}
