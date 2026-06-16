// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package selfupgrade_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/selfupgrade"
	"github.com/hashgraph/solo-weaver/pkg/schema"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sample() selfupgrade.SelfUpgradeYAML {
	return selfupgrade.SelfUpgradeYAML{
		Timestamp:         time.Date(2026, 6, 16, 10, 30, 0, 0, time.UTC),
		OperationID:       "op-2026-06-16-abc123",
		Status:            selfupgrade.StatusInProgress,
		ChildPID:          4242,
		CurrentStep:       "swap-cli-binary",
		FromCLIVersion:    "v1.1.0",
		ToCLIVersion:      "v1.2.3",
		FromDaemonVersion: "daemon-v1.1.0",
		ToDaemonVersion:   "daemon-v1.2.3",
		CLIBakPath:        "/opt/solo/weaver/backup/solo-provisioner/solo-provisioner-op-2026-06-16-abc123.bak",
		DaemonBakPath:     "/opt/solo/weaver/backup/solo-provisioner/solo-provisioner-daemon-op-2026-06-16-abc123.bak",
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "self-upgrade.yaml")
	want := sample()

	require.NoError(t, selfupgrade.Save(path, want))

	got, err := selfupgrade.Load(path)
	require.NoError(t, err)

	// Save stamps the schema version even though the in-memory sample left it 0.
	assert.Equal(t, selfupgrade.CurrentSchemaVersion, got.SchemaVersion)

	// Compare field-by-field; time.Time needs Equal (location/monotonic safety).
	assert.True(t, want.Timestamp.Equal(got.Timestamp), "timestamp round-trip")
	want.SchemaVersion = selfupgrade.CurrentSchemaVersion
	want.Timestamp = got.Timestamp // normalise for the struct-level compare below
	assert.Equal(t, want, got)
}

func TestSave_IsAtomicAndStampsVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "self-upgrade.yaml")
	require.NoError(t, selfupgrade.Save(path, sample()))

	// Parent dirs created; no leftover .tmp file.
	_, err := os.Stat(path)
	require.NoError(t, err)
	_, err = os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err), "temp file must not remain after rename")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "schemaVersion: 1")
}

func TestLoad_RejectsUnknownSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "self-upgrade.yaml")
	require.NoError(t, os.WriteFile(path,
		[]byte("schemaVersion: 99\noperationId: op-x\nstatus: in-progress\n"), 0o600))

	_, err := selfupgrade.Load(path)
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, schema.ErrUnsupportedVersion),
		"expected ErrUnsupportedVersion, got %T: %v", err, err)
}

func TestLoad_RejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "self-upgrade.yaml")
	require.NoError(t, os.WriteFile(path,
		[]byte("schemaVersion: 1\noperationId: op-x\nbogus: true\n"), 0o600))

	_, err := selfupgrade.Load(path)
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, schema.ErrMalformed),
		"expected ErrMalformed, got %T: %v", err, err)
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := selfupgrade.Load(filepath.Join(t.TempDir(), "absent.yaml"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, selfupgrade.ErrState),
		"expected ErrState, got %T: %v", err, err)
}
