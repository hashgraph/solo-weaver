// SPDX-License-Identifier: Apache-2.0

package network

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	require.NoError(t, os.WriteFile(path, []byte(`{"from":"file"}`), 0o600))

	h := &GenesisHandler{}
	out, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:   "orbit-a",
		GenesisFile: path,
	}))
	require.NoError(t, err)
	assert.JSONEq(t, `{"from":"file"}`, out.Custom.GenesisNetworkJSON)
}

func TestPrepareEffectiveInputs_ReadsDeploymentPackageGenesis(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, genesisNetworkFileName), []byte(`{"from":"pkg"}`), 0o600))

	h := &GenesisHandler{}
	out, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:            "orbit-a",
		DeploymentPackageDir: dir,
	}))
	require.NoError(t, err)
	assert.JSONEq(t, `{"from":"pkg"}`, out.Custom.GenesisNetworkJSON)
}

func TestPrepareEffectiveInputs_GenesisFileOverridesPackage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, genesisNetworkFileName), []byte(`{"from":"pkg"}`), 0o600))
	override := filepath.Join(dir, "override.json")
	require.NoError(t, os.WriteFile(override, []byte(`{"from":"override"}`), 0o600))

	h := &GenesisHandler{}
	out, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:            "orbit-a",
		DeploymentPackageDir: dir,
		GenesisFile:          override,
	}))
	require.NoError(t, err)
	assert.JSONEq(t, `{"from":"override"}`, out.Custom.GenesisNetworkJSON)
}

func TestPrepareEffectiveInputs_MissingGenesisFileErrors(t *testing.T) {
	h := &GenesisHandler{}
	_, err := h.PrepareEffectiveInputs(models.Intent{}, genesisInputs(models.ConsensusNetworkInputs{
		Namespace:   "orbit-a",
		GenesisFile: filepath.Join(t.TempDir(), "does-not-exist.json"),
	}))
	require.Error(t, err)
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
