// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/stretchr/testify/require"
)

// fakeStateManager is an in-memory state.Manager: Set/FlushState record the
// flushed state instead of writing a file, so a verb's decision write is
// observable without touching the host's real state (or needing the generated
// principal mocks that a real fsx.Manager would chown through).
type fakeStateManager struct {
	current  state.State
	flushed  *state.State
	scoped   [][]state.ComponentID // the ids of every FlushScoped call
	refresh  error
	flushErr error
}

func (f *fakeStateManager) State() state.State { return f.current }
func (f *fakeStateManager) HasPersistedState() (os.FileInfo, bool, error) {
	return nil, false, nil
}
func (f *fakeStateManager) Set(s state.State) state.Writer { f.current = s; return f }
func (f *fakeStateManager) AddActionHistory(state.ActionHistory) state.Writer {
	return f
}
func (f *fakeStateManager) FlushState() error {
	if f.flushErr != nil {
		return f.flushErr
	}
	snapshot := f.current
	f.flushed = &snapshot
	return nil
}
func (f *fakeStateManager) FlushScoped(ids ...state.ComponentID) error {
	f.scoped = append(f.scoped, ids)
	return f.FlushState()
}
func (f *fakeStateManager) FlushActionHistory() error { return nil }
func (f *fakeStateManager) FlushAll() error           { return f.FlushState() }
func (f *fakeStateManager) Refresh() error            { return f.refresh }
func (f *fakeStateManager) FileManager() fsx.Manager  { return nil }

// stubStateManager points the package's state seam at an in-memory manager and
// returns it, so a test can assert on what a verb flushed. Its state directory,
// where the verbs take the firewall lock, is a temp dir.
func stubStateManager(t *testing.T) *fakeStateManager {
	t.Helper()
	fake := &fakeStateManager{current: state.State{StateFile: filepath.Join(t.TempDir(), state.StateFileName)}}

	orig := stateManager
	stateManager = func() (state.Manager, error) { return fake, nil }
	t.Cleanup(func() { stateManager = orig })
	return fake
}

// flushedFirewall returns the host-firewall record the last flush persisted,
// failing the test when nothing was flushed at all.
func flushedFirewall(t *testing.T, fake *fakeStateManager) *state.HostFirewallState {
	t.Helper()
	require.NotNil(t, fake.flushed, "expected the verb to flush state")
	return fake.flushed.MachineState.Firewall
}

// TestCreateCmd_RecordsEnableDecision is the core of issue #1003: a firewall
// created through the standalone verb must leave the block node's persisted
// decision saying "enabled", or the next no-flag `block node reconfigure` reads
// the absent record as "disabled" and deletes the table.
func TestCreateCmd_RecordsEnableDecision(t *testing.T) {
	stubManager(t)
	fake := stubStateManager(t)

	require.NoError(t, run(t, "create", "--mgmt-cidrs", "192.168.50.0/24"))

	fw := flushedFirewall(t, fake)
	require.NotNil(t, fw, "create must record a host-firewall decision")
	require.False(t, fw.Disabled, "a created firewall must be recorded as enabled")
}

// TestCreateCmd_RecordsDecisionOnNoOp covers the create-if-missing no-op: the
// supplied flags were not applied, but "this host wants a firewall" is still
// true, and that is the only thing the decision records.
func TestCreateCmd_RecordsDecisionOnNoOp(t *testing.T) {
	stubManager(t)
	fake := stubStateManager(t)

	require.NoError(t, run(t, "create", "--mgmt-cidrs", "192.168.50.0/24"))
	// Second create without --force: the table exists, so nothing is re-rendered.
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "10.0.0.0/8"))

	fw := flushedFirewall(t, fake)
	require.NotNil(t, fw)
	require.False(t, fw.Disabled)
}

// TestDeleteCmd_RecordsDisableDecision covers the inverse of #1003: after a
// standalone teardown, a machine state still saying "enabled" would make the
// next reconfigure re-create the table the operator just removed.
func TestDeleteCmd_RecordsDisableDecision(t *testing.T) {
	stubManager(t)
	fake := stubStateManager(t)

	require.NoError(t, run(t, "create", "--mgmt-cidrs", "192.168.50.0/24"))
	require.NoError(t, run(t, "delete", "--all"))

	fw := flushedFirewall(t, fake)
	require.NotNil(t, fw)
	require.True(t, fw.Disabled, "a deleted firewall must be recorded as disabled")
}

// TestDeleteCmd_PreservesRecordedContent verifies the decision write leaves the
// last-known-good allowlist in place, so a later bare re-enable still restores it
// (issue #932) instead of skipping with the SSH-lockout guard.
func TestDeleteCmd_PreservesRecordedContent(t *testing.T) {
	stubManager(t)
	fake := stubStateManager(t)

	// Seed the record the block-node workflow would have written.
	fake.current.MachineState.Firewall = &state.HostFirewallState{
		ManagementCIDRs: []string{"192.168.50.0/24"},
		MgmtPorts:       []int{2222},
	}

	require.NoError(t, run(t, "delete", "--all"))

	fw := flushedFirewall(t, fake)
	require.NotNil(t, fw)
	require.True(t, fw.Disabled)
	require.Equal(t, []string{"192.168.50.0/24"}, fw.ManagementCIDRs, "content must survive the decision write")
	require.Equal(t, []int{2222}, fw.MgmtPorts)
}

// TestDeleteCmd_ByNameLeavesDecisionAlone verifies that removing one allow rule
// is not read as a decision to disable the firewall.
func TestDeleteCmd_ByNameLeavesDecisionAlone(t *testing.T) {
	stubManager(t)
	fake := stubStateManager(t)

	rules := filepath.Join(t.TempDir(), "rules.yaml")
	require.NoError(t, os.WriteFile(rules, []byte(`version: 1
mgmt:
  cidrs: ["192.168.50.0/24"]
blocked:
  cidrs: []
in_cluster:
  cidrs: []
allow:
  - name: k8s-node
    cidrs: ["10.0.0.0/24"]
    ports: ["6443"]
`), 0o600))

	require.NoError(t, run(t, "create", "--from-file", rules))
	fake.flushed = nil // only observe what the rule delete does

	require.NoError(t, run(t, "delete", "--name", "k8s-node"))
	require.Nil(t, fake.flushed, "deleting one allow rule is not a decision to disable the firewall")
}

// TestCreateCmd_FailsBeforeApplyingWhenStateIsUnavailable: with no state to
// record the decision in, create stops before it touches the kernel.
func TestCreateCmd_FailsBeforeApplyingWhenStateIsUnavailable(t *testing.T) {
	nftPath, _ := stubManager(t)

	orig := stateManager
	stateManager = func() (state.Manager, error) { return nil, errors.New("no state manager") }
	t.Cleanup(func() { stateManager = orig })

	require.Error(t, run(t, "create", "--mgmt-cidrs", "192.168.50.0/24"))
	require.NoFileExists(t, nftPath, "no ruleset may be applied when the decision cannot be recorded")
}

// TestFirewallVerbs_FailFastBeforeApplyingWhenTheFirewallIsLocked: another
// command holding the firewall lock makes create and delete fail at once,
// before they change the kernel or the recorded decision.
func TestFirewallVerbs_FailFastBeforeApplyingWhenTheFirewallIsLocked(t *testing.T) {
	nftPath, _ := stubManager(t)
	fake := stubStateManager(t)
	stateDir := filepath.Dir(fake.current.StateFile)

	hold := func() func() {
		release, err := state.AcquireComponentLocks(stateDir, []state.ComponentID{state.ComponentFirewall}, 0)
		require.NoError(t, err)
		return release
	}

	release := hold()
	err := run(t, "create", "--mgmt-cidrs", "192.168.50.0/24")
	release()
	require.ErrorContains(t, err, "firewall is being modified")
	require.NoFileExists(t, nftPath, "create must not apply the ruleset while the firewall is locked")
	require.Nil(t, fake.flushed, "create must not record a decision while the firewall is locked")

	require.NoError(t, run(t, "create", "--mgmt-cidrs", "192.168.50.0/24"))
	fake.flushed = nil

	release = hold()
	err = run(t, "delete", "--all")
	release()
	require.ErrorContains(t, err, "firewall is being modified")
	require.FileExists(t, nftPath, "delete must not remove the ruleset while the firewall is locked")
	require.Nil(t, fake.flushed, "delete must not record a decision while the firewall is locked")
}

// TestFirewallVerbs_WriteOnlyTheFirewallFile: recording the decision touches
// the firewall component alone, never the other component files.
func TestFirewallVerbs_WriteOnlyTheFirewallFile(t *testing.T) {
	stubManager(t)
	fake := stubStateManager(t)

	require.NoError(t, run(t, "create", "--mgmt-cidrs", "192.168.50.0/24"))
	require.NoError(t, run(t, "delete", "--all"))

	require.Len(t, fake.scoped, 2)
	for _, ids := range fake.scoped {
		require.Equal(t, []state.ComponentID{state.ComponentFirewall}, ids)
	}
}

// TestCreateCmd_FailsWhenTheDecisionCannotBeRecorded: once the ruleset is
// applied, a failed record is an error the operator sees, with the way to
// recover, never only a log line. A dropped decision is what makes the next
// reconfigure undo this command.
func TestCreateCmd_FailsWhenTheDecisionCannotBeRecorded(t *testing.T) {
	nftPath, _ := stubManager(t)
	fake := stubStateManager(t)
	fake.flushErr = errors.New("disk full")

	err := run(t, "create", "--mgmt-cidrs", "192.168.50.0/24")
	require.Error(t, err)
	require.FileExists(t, nftPath, "the ruleset was applied before the record failed")

	reason, ok := errx.ReasonOf(err)
	require.True(t, ok, "no reason attached")
	require.Equal(t, reasons.StateOperationFailed, reason)
	hints, ok := errx.Hints(err)
	require.True(t, ok, "no hints attached")
	require.Len(t, hints, 1)
	require.Contains(t, hints[0], "Re-run the same command")

	// Doing what the hint says records the decision.
	fake.flushErr = nil
	require.NoError(t, run(t, "create", "--mgmt-cidrs", "192.168.50.0/24"))
	fw := flushedFirewall(t, fake)
	require.NotNil(t, fw)
	require.False(t, fw.Disabled, "the re-run must record the enable decision")
}
