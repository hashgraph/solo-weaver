// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package bll

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/stretchr/testify/require"
)

// writeFirewallRecord seeds a state file whose recorded firewall decision is record.
func writeFirewallRecord(t *testing.T, stateFile string, record *state.HostFirewallState) {
	t.Helper()
	s := state.NewState(stateFile)
	s.MachineState.Firewall = record
	sm, err := state.NewStateManager(state.WithState(s), state.WithFileManager(newFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.Commit(state.ActionHistory{}, state.AllComponentIDs...))
	require.NoError(t, os.Remove(state.ActionHistoryFilePath(filepath.Dir(stateFile))))
}

func TestHandleIntent_FirewallOwnerReadsTheHostOnceAndReportsIt(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeFirewallRecord(t, stateFile, &state.HostFirewallState{Disabled: true})
	h := &host{firewall: &state.HostFirewallObservation{TablePresent: true, ConfigPresent: true}}

	report, err := handleBlockNodeIntentWith(t, stateFile, h, noopIntent{}, Firewall)
	require.NoError(t, err)
	require.NoError(t, report.Error)

	require.Equal(t, []string{
		`firewall disabled differs from persisted state: recorded "true", live "false"`,
		drift.FirewallAdvice([]drift.Change{{Component: "firewall", Field: "disabled"}})[0],
	}, ui.CollectWarnings(report))
	require.Equal(t, 1, h.firewallReads)
	// Recorded once, from the pre-workflow reading: the run neither destroys it
	// nor reads it again at the flush.
	entries := readActionHistory(t, filepath.Dir(stateFile))
	require.Len(t, entries, 1)
	require.Equal(t, []state.OwnedDriftRecord{{
		Component: "firewall", Field: "disabled", Persisted: "true", Live: "false",
	}}, entries[0].OwnedDrift)
}

func TestHandleIntent_NoStateFileNeverReadsTheFirewall(t *testing.T) {
	// With no baseline there is nothing to compare, so nft is not run.
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	h := &host{firewall: &state.HostFirewallObservation{TablePresent: true}}

	report, err := handleBlockNodeIntentWith(t, stateFile, h, noopIntent{}, Firewall)
	require.NoError(t, err)
	require.NoError(t, report.Error)

	require.Zero(t, h.firewallReads)
}

func TestHandleIntent_HandlerNotOwningTheFirewallNeverReadsIt(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeFirewallRecord(t, stateFile, &state.HostFirewallState{Disabled: true})
	h := &host{firewall: &state.HostFirewallObservation{TablePresent: true}}

	report, err := handleBlockNodeIntentWith(t, stateFile, h, noopIntent{}, Teleport)
	require.NoError(t, err)
	require.NoError(t, report.Error)

	require.Empty(t, ui.CollectWarnings(report))
	require.Zero(t, h.firewallReads)
}

func TestHandleIntent_NoFirewallReaderIsSilent(t *testing.T) {
	// Checkers built by hand have no reader, so no unit test runs nft.
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeFirewallRecord(t, stateFile, &state.HostFirewallState{Disabled: true})

	report, err := handleBlockNodeIntentWith(t, stateFile, &host{}, noopIntent{}, Firewall)
	require.NoError(t, err)
	require.NoError(t, report.Error)

	require.Empty(t, ui.CollectWarnings(report))
}

func TestHandleIntent_UnreadFirewallWarnsAndTheRunGoesAhead(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeFirewallRecord(t, stateFile, &state.HostFirewallState{})
	h := &host{firewall: &state.HostFirewallObservation{Unobserved: true, ConfigPresent: true}}
	ran := false

	report, err := handleBlockNodeIntentWith(t, stateFile, h, noopIntent{execute: func() { ran = true }}, Firewall)
	require.NoError(t, err)
	require.NoError(t, report.Error)

	require.True(t, ran)
	require.Equal(t, []string{
		`firewall configFile could not be read from the live system: recorded "present"`,
		drift.FirewallAdvice([]drift.Change{{Component: "firewall", Unobserved: true}})[0],
	}, ui.CollectWarnings(report))
}
