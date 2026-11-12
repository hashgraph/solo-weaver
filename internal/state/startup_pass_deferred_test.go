// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/stretchr/testify/require"
)

var apiDown = migration.Deferral{
	ID:    "cilium-host-legacy-routing-v0.21.0",
	Cause: "cannot read kube-system/cilium-config: connection refused",
	Hints: []string{"Check the API server: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get --raw /readyz"},
}

// TestRecordStartupPass_DeferralWithholdsTheStampAndWarns: a deferred
// migration has not run, so the version stays where it was for the next
// invocation to retry it. A pass that only deferred changed no state file, so
// it records nothing; the operator learns of it from the warning.
func TestRecordStartupPass_DeferralWithholdsTheStampAndWarns(t *testing.T) {
	_ = TakeWarnings()
	dir, pass := passDir(t)
	stamped := false
	stamp := func() (bool, error) { stamped = true; return true, nil }

	require.NoError(t, RecordStartupPass(context.Background(), dir, pass, stamp, func() (StartupPassRun, error) {
		return StartupPassRun{Deferred: []migration.Deferral{apiDown}}, nil
	}))

	require.False(t, stamped, "a pass with a deferral must not record the provisioner version")
	require.Empty(t, passHistory(t, dir))
	warnings := TakeWarnings()
	require.Equal(t, []string{"Startup migration cilium-host-legacy-routing-v0.21.0 was deferred and will run on a later command: " +
		"cannot read kube-system/cilium-config: connection refused. " +
		"Check the API server: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get --raw /readyz"}, warnings)
}

// TestRecordStartupPass_EntryListsDeferredMigrations: when another migration in
// the pass changed a state file, its entry names the deferred ones apart from
// those applied, and does not credit the version, which was not recorded.
func TestRecordStartupPass_EntryListsDeferredMigrations(t *testing.T) {
	_ = TakeWarnings()
	dir, pass := passDir(t)
	stamp := func() (bool, error) { return true, nil }

	require.NoError(t, RecordStartupPass(context.Background(), dir, pass, stamp, func() (StartupPassRun, error) {
		return StartupPassRun{Migrations: []string{"mgmt-ports-v1"}, Deferred: []migration.Deferral{apiDown}},
			os.WriteFile(filepath.Join(dir, StateFileName), []byte(mgmtPortsYAML), 0o644)
	}))

	entries := passHistory(t, dir)
	require.Len(t, entries, 1)
	require.Equal(t, OutcomeSucceeded, entries[0].Outcome)
	require.Equal(t, []ChangedFile{ChangedLegacy}, entries[0].Components, "the version was not recorded")
	inputs, ok := entries[0].Inputs.(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{"mgmt-ports-v1"}, inputs["migrations"])
	require.Equal(t, []any{"cilium-host-legacy-routing-v0.21.0"}, inputs["deferred"])
	require.Len(t, TakeWarnings(), 1)
}

// TestRecordStartupPass_DeferralWarnsWhenThePassFails: a later failure must
// not hide that a migration is still waiting to run.
func TestRecordStartupPass_DeferralWarnsWhenThePassFails(t *testing.T) {
	_ = TakeWarnings()
	dir, pass := passDir(t)

	require.Error(t, RecordStartupPass(context.Background(), dir, pass, nil, func() (StartupPassRun, error) {
		return StartupPassRun{Outcome: OutcomeFailed, Failed: "sysctl-network-tuning", Deferred: []migration.Deferral{apiDown}}, errors.New("sysctl --system failed")
	}))

	warnings := TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "cilium-host-legacy-routing-v0.21.0")
}
