// SPDX-License-Identifier: Apache-2.0

package common

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/automa-saga/logx"
	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiGatedMigration defers until apiUp is set, then applies.
type apiGatedMigration struct {
	fakeStartupMigration
	apiUp *bool
}

func (m *apiGatedMigration) Execute(ctx context.Context, mctx *migration.Context) error {
	if !*m.apiUp {
		return migration.ErrDeferred.New("the Kubernetes API is not available")
	}
	return m.fakeStartupMigration.Execute(ctx, mctx)
}

// TestRunStartupMigrations_DeferredMigrationRunsOnALaterCommand is the issue's
// scenario: the first command after an upgrade meets a down API, so the
// migration defers, the command still runs, and the version stays where it
// was, across a later upgrade too; the first command after the API returns
// applies it.
func TestRunStartupMigrations_DeferredMigrationRunsOnALaterCommand(t *testing.T) {
	migration.ClearRegistry()
	t.Cleanup(migration.ClearRegistry)
	origVersion := version.Version
	t.Cleanup(func() { version.Version = origVersion })
	version.Version = "1.2.3"
	t.Cleanup(models.SetPaths(t.TempDir()))
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))
	origK8s := workflows.KubernetesInstalled
	t.Cleanup(func() { workflows.KubernetesInstalled = origK8s })
	workflows.KubernetesInstalled = func() bool { return true }
	_ = state.TakeWarnings()

	applied, apiUp := 0, false
	migration.Register(migration.ScopeStartup, &apiGatedMigration{
		fakeStartupMigration: fakeStartupMigration{id: "cilium-host-legacy-routing-v0.21.0", applied: &applied},
		apiUp:                &apiUp,
	})
	ctx := context.Background()

	require.NoError(t, RunStartupMigrations(ctx, ""), "a deferral must not block the command")
	recorded, err := state.ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Empty(t, recorded, "the version must not be recorded while a migration is deferred")
	warnings := state.TakeWarnings()
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "cilium-host-legacy-routing-v0.21.0")

	require.NoError(t, RunStartupMigrations(ctx, ""))
	assert.Zero(t, applied, "still deferred while the API is down")
	assert.Len(t, state.TakeWarnings(), 1, "every command warns until the deferral clears")

	// The operator upgrades again before the API comes back: the deferral
	// carries over to the new binary.
	version.Version = "1.3.0"
	require.NoError(t, RunStartupMigrations(ctx, ""))
	assert.Zero(t, applied)
	assert.Len(t, state.TakeWarnings(), 1)

	apiUp = true
	require.NoError(t, RunStartupMigrations(ctx, ""))
	assert.Equal(t, 1, applied, "the first command after the API returns applies it")
	recorded, err = state.ReadProvisionerVersionFromDisk()
	require.NoError(t, err)
	assert.Equal(t, "1.3.0", recorded)
	assert.Empty(t, state.TakeWarnings())
}

// TestRunStartupMigrations_DeferralLogsNameTheCommand: the pass's log lines
// say which command ran it, whether systemd started it (the DNS-refresh timer),
// and how long it took, both when it defers and when a later pass applies the
// change; a deferral carries a reason to filter on across rotated log files.
func TestRunStartupMigrations_DeferralLogsNameTheCommand(t *testing.T) {
	migration.ClearRegistry()
	t.Cleanup(migration.ClearRegistry)
	t.Cleanup(models.SetPaths(t.TempDir()))
	require.NoError(t, os.MkdirAll(models.Paths().StateDir, 0o755))
	origK8s := workflows.KubernetesInstalled
	t.Cleanup(func() { workflows.KubernetesInstalled = origK8s })
	workflows.KubernetesInstalled = func() bool { return true }
	t.Cleanup(func() { _ = state.TakeWarnings() })

	apiUp := false
	migration.Register(migration.ScopeStartup, &apiGatedMigration{
		fakeStartupMigration: fakeStartupMigration{id: "cilium-host-legacy-routing-v0.21.0", applied: new(int)},
		apiUp:                &apiUp,
	})

	buf := captureLogs(t)

	// logLines runs one pass and returns its log lines by message.
	logLines := func(command string) map[string]map[string]any {
		buf.Reset()
		require.NoError(t, RunStartupMigrations(context.Background(), command))
		return logLinesByMessage(t, buf)
	}

	// The timer's run: systemd sets INVOCATION_ID for a service.
	t.Setenv("INVOCATION_ID", "0123456789abcdef")
	const timer = "solo-provisioner network firewall refresh-dns"
	lines := logLines(timer)

	start := lines["Running startup migrations..."]
	require.NotNil(t, start, "logs: %s", buf.String())
	assert.Equal(t, timer, start["command"])
	assert.Equal(t, true, start["systemd"])

	done := lines["Startup migrations completed; some were deferred to a later command"]
	require.NotNil(t, done, "logs: %s", buf.String())
	assert.Equal(t, timer, done["command"])
	assert.Contains(t, done, "elapsed")

	deferred := lines["Migration deferred; it will run on a later invocation"]
	require.NotNil(t, deferred, "logs: %s", buf.String())
	assert.Equal(t, "StartupMigrationDeferred", deferred["reason"])

	// An operator's command after the API returns applies the change.
	t.Setenv("INVOCATION_ID", "")
	apiUp = true
	const operator = "solo-provisioner kube"
	lines = logLines(operator)

	start = lines["Running startup migrations..."]
	require.NotNil(t, start, "logs: %s", buf.String())
	assert.Equal(t, false, start["systemd"])

	applied := lines["Startup migrations completed successfully"]
	require.NotNil(t, applied, "logs: %s", buf.String())
	assert.Equal(t, operator, applied["command"])
	assert.Contains(t, applied, "elapsed")
}

// captureLogs sends logx to a buffer at DEBUG level for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	origLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(origLevel) })
	buf := new(bytes.Buffer)
	origLogger := *logx.As()
	logx.SetLogger(zerolog.New(buf))
	t.Cleanup(func() { logx.SetLogger(origLogger) })
	return buf
}

// logLinesByMessage decodes the captured JSON log lines, keyed by message.
func logLinesByMessage(t *testing.T, buf *bytes.Buffer) map[string]map[string]any {
	t.Helper()
	lines := map[string]map[string]any{}
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m), "not JSON: %q", l)
		msg, _ := m["message"].(string)
		lines[msg] = m
	}
	return lines
}
