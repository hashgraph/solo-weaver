// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runPass(t *testing.T, mctx *Context, migrations ...Migration) *automa.Report {
	t.Helper()
	wf, err := MigrationsToWorkflow(migrations, mctx).Build()
	require.NoError(t, err)
	return wf.Execute(context.Background())
}

func deferring(id string) *MockMigration {
	m := NewMockMigration(id, "1.0.0")
	m.executeFunc = func(context.Context, *Context) error {
		read := errorx.ExternalError.Wrap(errors.New("connection refused"), "cannot read kube-system/cilium-config")
		return errx.Decorate(ErrDeferred.Wrap(read, "the Kubernetes API is not available"),
			reasons.K8sAPIFailed, "Check the API server: kubectl get --raw /readyz")
	}
	return m
}

func TestMigrationsToWorkflow_DeferredMigrationIsNeitherAppliedNorFailed(t *testing.T) {
	mctx := &Context{Component: ScopeStartup, Logger: testLogger()}

	report := runPass(t, mctx, NewMockMigration("crio-socket-dropin", "1.0.0"), deferring("cilium-host-legacy-routing-v0.21.0"))

	require.NoError(t, report.Error, "a deferral does not fail the pass")
	applied, rolledBack, failed := RanIDs(report)
	assert.Equal(t, []string{"crio-socket-dropin"}, applied)
	assert.Empty(t, rolledBack)
	assert.Empty(t, failed)

	deferred := mctx.Deferred()
	require.Len(t, deferred, 1)
	assert.Equal(t, "cilium-host-legacy-routing-v0.21.0", deferred[0].ID)
	assert.Equal(t, "the Kubernetes API is not available: common.external_error: cannot read kube-system/cilium-config, cause: connection refused",
		deferred[0].Cause, "the sentinel's type name and the printable reason are dropped; inner type names stay")
	assert.Equal(t, []string{"Check the API server: kubectl get --raw /readyz"}, deferred[0].Hints)
}

func TestMigrationsToWorkflow_LaterFailureDoesNotRollBackADeferredMigration(t *testing.T) {
	mctx := &Context{Component: ScopeStartup, Logger: testLogger()}
	applied := NewMockMigration("crio-socket-dropin", "1.0.0")
	deferred := deferring("cilium-disable-xdp-acceleration-v0.19.1")
	broken := NewMockMigration("sysctl-network-tuning", "1.0.0")
	broken.executeFunc = func(context.Context, *Context) error { return errors.New("sysctl --system failed") }

	report := runPass(t, mctx, applied, deferred, broken)

	require.Error(t, report.Error)
	assert.Equal(t, 1, applied.rollbackCalls, "a migration that ran is rolled back")
	assert.Zero(t, deferred.rollbackCalls, "a migration that deferred never ran, so there is nothing to undo")
	_, rolledBack, failed := RanIDs(report)
	assert.Equal(t, []string{"crio-socket-dropin"}, rolledBack)
	assert.Equal(t, "sysctl-network-tuning", failed)
	assert.Len(t, mctx.Deferred(), 1, "the deferral is still reported when the pass fails")
}

func TestMigrationsToWorkflow_DeferralFailsOutsideTheStartupScope(t *testing.T) {
	// The block-node upgrade appends the chart upgrade after its migrations, so
	// one skipped there would upgrade the chart without its storage.
	mctx := &Context{Component: ScopeBlockNode, Logger: testLogger()}

	report := runPass(t, mctx, deferring("verification-storage-v0.26.2"))

	require.Error(t, report.Error)
	_, _, failed := RanIDs(report)
	assert.Equal(t, "verification-storage-v0.26.2", failed)
	assert.Empty(t, mctx.Deferred())
}

func TestMigrationsToWorkflow_LaterMigrationSeesAnEarlierDeferral(t *testing.T) {
	mctx := &Context{Component: ScopeStartup, Logger: testLogger()}
	var seen []Deferral
	restart := NewMockMigration("cilium-agent-restart-disabled-acceleration-v0.19.2", "1.0.0")
	restart.executeFunc = func(_ context.Context, mctx *Context) error {
		seen = mctx.Deferred()
		return nil
	}

	runPass(t, mctx, deferring("cilium-host-legacy-routing-v0.21.0"), restart)

	require.Len(t, seen, 1)
	assert.Equal(t, "cilium-host-legacy-routing-v0.21.0", seen[0].ID)
}

func TestMigrationsToWorkflow_WrappedDeferralFails(t *testing.T) {
	// A deferral under another error may come after a change, so it stays a
	// failure the operator sees rather than a silent skip with no rollback.
	mctx := &Context{Component: ScopeStartup, Logger: testLogger()}
	m := NewMockMigration("cilium-disable-xdp-acceleration-v0.19.1", "1.0.0")
	m.executeFunc = func(context.Context, *Context) error {
		return errorx.IllegalState.Wrap(ErrDeferred.New("api down"), "failed to run 'cilium upgrade'")
	}

	report := runPass(t, mctx, m)

	require.Error(t, report.Error)
	assert.Empty(t, mctx.Deferred())
}
