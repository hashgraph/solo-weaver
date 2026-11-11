// SPDX-License-Identifier: Apache-2.0

package network

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/automa-saga/errx"
	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Hex and base64 forms of one Ed25519 key, and a second key.
const (
	adminKeyHex  = "0aa8e21064c61eab86e2a9c164565b4e7a9a4146106e0a6cd03a8c395a110e92"
	adminKeyB64  = "CqjiEGTGHquG4qnBZFZbTnqaQUYQbgps0DqMOVoRDpI="
	otherKeyHex  = "1111111111111111111111111111111111111111111111111111111111111111"
	noAdminKey   = ""
	genesisLabel = "description"
)

// genesisJSON is a one-node (node 0) genesis-network.json; label tells the copies apart.
func genesisJSON(label, adminKeyB64 string) string {
	key := ""
	if adminKeyB64 != "" {
		key = fmt.Sprintf(`,"adminKey":{"ed25519":%q}`, adminKeyB64)
	}
	return fmt.Sprintf(`{"nodeMetadata":[{"rosterEntry":{"weight":"1"},"node":{%q:%q%s}}]}`, genesisLabel, label, key)
}

func writeNodeAdminKeys(t *testing.T, pkgDir, content string) {
	t.Helper()
	path := filepath.Join(pkgDir, cnkeys.NodeAdminKeysPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func genesisInputs(c models.ConsensusNetworkInputs) models.UserInputs[models.ConsensusNetworkInputs] {
	return models.UserInputs[models.ConsensusNetworkInputs]{Custom: c}
}

func TestPrepareEffectiveInputs_OrbitDefaultsToNamespace(t *testing.T) {
	h := &GenesisHandler{}
	out, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace: "hiero-network-1",
	}))
	require.NoError(t, err)
	assert.Equal(t, "hiero-network-1", out.Custom.Orbit)
	assert.Empty(t, out.Custom.GenesisNetworkJSON, "no genesis-file/pkg-dir ⇒ cluster discovery (empty JSON)")
}

func TestPrepareEffectiveInputs_ReadsGenesisFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-genesis.json")
	require.NoError(t, os.WriteFile(path, []byte(genesisJSON("file", adminKeyB64)), 0o600))

	h := &GenesisHandler{}
	out, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:   "orbit-a",
		GenesisFile: path,
	}))
	require.NoError(t, err)
	assert.JSONEq(t, genesisJSON("file", adminKeyB64), out.Custom.GenesisNetworkJSON)
}

func TestPrepareEffectiveInputs_ReadsDeploymentPackageGenesis(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, genesisNetworkFileName), []byte(genesisJSON("pkg", adminKeyB64)), 0o600))

	h := &GenesisHandler{}
	out, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:            "orbit-a",
		DeploymentPackageDir: dir,
	}))
	require.NoError(t, err)
	assert.JSONEq(t, genesisJSON("pkg", adminKeyB64), out.Custom.GenesisNetworkJSON)
}

func TestPrepareEffectiveInputs_GenesisFileOverridesPackage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, genesisNetworkFileName), []byte(genesisJSON("pkg", adminKeyB64)), 0o600))
	override := filepath.Join(dir, "override.json")
	require.NoError(t, os.WriteFile(override, []byte(genesisJSON("override", adminKeyB64)), 0o600))

	h := &GenesisHandler{}
	out, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:            "orbit-a",
		DeploymentPackageDir: dir,
		GenesisFile:          override,
	}))
	require.NoError(t, err)
	assert.JSONEq(t, genesisJSON("override", adminKeyB64), out.Custom.GenesisNetworkJSON)
}

func TestPrepareEffectiveInputs_MissingGenesisFileErrors(t *testing.T) {
	h := &GenesisHandler{}
	_, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:   "orbit-a",
		GenesisFile: filepath.Join(t.TempDir(), "does-not-exist.json"),
	}))
	require.Error(t, err)
}

func TestPrepareEffectiveInputs_AdminKeysFromPackageFillGenesis(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, genesisNetworkFileName), []byte(genesisJSON("pkg", noAdminKey)), 0o600))
	keys := fmt.Sprintf(`{"0":%q}`, adminKeyHex)
	writeNodeAdminKeys(t, dir, keys)

	h := &GenesisHandler{}
	out, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:            "orbit-a",
		DeploymentPackageDir: dir,
	}))
	require.NoError(t, err)
	assert.JSONEq(t, keys, out.Custom.NodeAdminKeysJSON)
}

func TestPrepareEffectiveInputs_AdminKeysFromPackageWithGenesisFileOverride(t *testing.T) {
	dir := t.TempDir()
	writeNodeAdminKeys(t, dir, fmt.Sprintf(`{"0":%q}`, adminKeyHex))
	override := filepath.Join(dir, "override.json")
	require.NoError(t, os.WriteFile(override, []byte(genesisJSON("override", noAdminKey)), 0o600))

	h := &GenesisHandler{}
	out, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:            "orbit-a",
		DeploymentPackageDir: dir,
		GenesisFile:          override,
	}))
	require.NoError(t, err, "the package keys file applies to the --genesis-file genesis")
	assert.NotEmpty(t, out.Custom.NodeAdminKeysJSON)
}

func TestPrepareEffectiveInputs_RejectsNodeWithoutAdminKey(t *testing.T) {
	tests := []struct {
		name string
		pkg  bool
	}{
		{"genesis file only", false},
		{"package without keys file", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			in := models.ConsensusNetworkInputs{Namespace: "orbit-a"}
			if tt.pkg {
				require.NoError(t, os.WriteFile(filepath.Join(dir, genesisNetworkFileName), []byte(genesisJSON("pkg", noAdminKey)), 0o600))
				in.DeploymentPackageDir = dir
			} else {
				in.GenesisFile = filepath.Join(dir, "genesis.json")
				require.NoError(t, os.WriteFile(in.GenesisFile, []byte(genesisJSON("file", noAdminKey)), 0o600))
			}

			h := &GenesisHandler{}
			_, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(in))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "no admin key for node(s) 0")
			reason, _ := errx.ReasonOf(err)
			assert.Equal(t, reasons.InvalidArgument, reason)
			hints, _ := errx.Hints(err)
			assert.NotEmpty(t, hints)
		})
	}
}

func TestPrepareEffectiveInputs_RejectsDifferentAdminKeys(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, genesisNetworkFileName), []byte(genesisJSON("pkg", adminKeyB64)), 0o600))
	writeNodeAdminKeys(t, dir, fmt.Sprintf(`{"0":%q}`, otherKeyHex))

	h := &GenesisHandler{}
	_, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:            "orbit-a",
		DeploymentPackageDir: dir,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "differ for node(s) 0")
}

func TestPrepareEffectiveInputs_RejectsEmptyAdminKeysFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, genesisNetworkFileName), []byte(genesisJSON("pkg", adminKeyB64)), 0o600))
	writeNodeAdminKeys(t, dir, " \n")

	h := &GenesisHandler{}
	_, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:            "orbit-a",
		DeploymentPackageDir: dir,
	}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is empty")
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.InvalidArgument, reason)
}

func TestPrepareEffectiveInputs_AdminKeysPathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, genesisNetworkFileName), []byte(genesisJSON("pkg", adminKeyB64)), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, cnkeys.NodeAdminKeysPath), 0o755))

	h := &GenesisHandler{}
	_, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:            "orbit-a",
		DeploymentPackageDir: dir,
	}))
	require.Error(t, err)
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.InvalidArgument, reason, "a directory is not a missing file")
}

func TestPrepareEffectiveInputs_DiscoverySkipsAdminKeyChecks(t *testing.T) {
	h := &GenesisHandler{}
	out, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{Namespace: "orbit-a"}))
	require.NoError(t, err)
	assert.Empty(t, out.Custom.NodeAdminKeysJSON)
}

func TestBuildWorkflow_BuildsWithAndWithoutReadyWait(t *testing.T) {
	h := &GenesisHandler{}

	// ReadyTimeout == 0 ⇒ apply-only (no readiness waits).
	wb, err := h.BuildWorkflow(state.State{}, genesisInputs(models.ConsensusNetworkInputs{Namespace: "orbit-a"}))
	require.NoError(t, err)
	_, err = wb.Build()
	require.NoError(t, err)

	// ReadyTimeout > 0 ⇒ adds the genesis + network readiness waits.
	wb, err = h.BuildWorkflow(state.State{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:    "orbit-a",
		ReadyTimeout: 30 * time.Second,
	}))
	require.NoError(t, err)
	_, err = wb.Build()
	require.NoError(t, err)
}
