// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestReadActionHistory_NoFileIsNoHistory(t *testing.T) {
	entries, err := ReadActionHistory(t.TempDir())
	require.NoError(t, err)
	require.Empty(t, entries)
}

// TestReadActionHistory_ReadsWhatCommitAppended: the reader returns every
// entry, oldest first, with what each recorded.
func TestReadActionHistory_ReadsWhatCommitAppended(t *testing.T) {
	dir := t.TempDir()
	m, err := NewStateManager(WithState(newTestState(filepath.Join(dir, StateFileName))), WithFileManager(newTestFileManager(t)))
	require.NoError(t, err)
	install := models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}
	upgrade := models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlockNode}

	entry := NewEntry(context.Background(), install, map[string]any{"profile": "local"})
	entry.Outcome = OutcomeSucceeded
	require.NoError(t, m.Commit(entry, ComponentBlockNode))
	entry = NewEntry(context.Background(), upgrade, nil)
	entry.Outcome = OutcomeFailed
	require.NoError(t, m.Commit(entry, ComponentBlockNode))

	entries, err := ReadActionHistory(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, install, entries[0].Intent)
	require.Equal(t, map[string]any{"profile": "local"}, entries[0].Inputs)
	require.Equal(t, []ChangedFile{ComponentBlockNode.File()}, entries[0].Components)
	require.Equal(t, upgrade, entries[1].Intent)
	require.Equal(t, OutcomeFailed, entries[1].Outcome)
}

// TestReadActionHistory_SkipsAnEntryCutShort: an entry a full disk cut short
// can't be decoded, but the entries around it still can.
func TestReadActionHistory_SkipsAnEntryCutShort(t *testing.T) {
	dir := t.TempDir()
	history := "\n---\nintent:\n  action: install\n  target: firewall\noutcome: succeeded\n" +
		"\n---\nintent:\n  action: [unterminated\n" +
		"\n---\nintent:\n  action: uninstall\n  target: firewall\noutcome: succeeded\n"
	require.NoError(t, os.WriteFile(ActionHistoryFilePath(dir), []byte(history), 0o644))

	entries, err := ReadActionHistory(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, models.ActionInstall, entries[0].Intent.Action)
	require.Equal(t, models.ActionUninstall, entries[1].Intent.Action)
}

// TestTargetFiles_EveryWriterTargetHasARow: each target that writes state
// names its files, and the targets that remove or migrate everything name
// every state file.
func TestTargetFiles_EveryWriterTargetHasARow(t *testing.T) {
	for _, target := range []models.TargetType{
		models.TargetBlockNode, models.TargetConsensusNode, models.TargetCluster,
		models.TargetTeleportNode, models.TargetTeleportCluster, models.TargetFirewall, models.TargetSystem,
		models.TargetAlloy, models.TargetESO,
	} {
		require.NotEmpty(t, TargetFiles(target), target)
	}
	require.Equal(t, everyStateFile(), TargetFiles(models.TargetCluster))
	require.Equal(t, everyStateFile(), TargetFiles(models.TargetSystem))
	require.Empty(t, TargetFiles(models.TargetMirrorNode))
}

// TestRunOwning_RefusesAFileItsTargetDoesNotOwn: a writer can't lock and write
// a file its target's row leaves out, so the table stays the truth.
func TestRunOwning_RefusesAFileItsTargetDoesNotOwn(t *testing.T) {
	sm, _ := newOwnedTestManager(t)
	ran := false

	err := RunOwning(sm, firewallEntry, []ComponentID{ComponentBlockNode},
		func() error { ran = true; return nil },
		func(*State) error { return nil })

	require.ErrorContains(t, err, `target "firewall" does not own the blocknode state file`)
	require.False(t, ran)
}
