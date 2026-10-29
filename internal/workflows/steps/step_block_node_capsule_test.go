// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyInstalledFn returns a legacyInstalled closure for PrecheckBlockNodeNotInstalled
// that reports the given state without touching a cluster.
func legacyInstalledFn(installed bool, err error) func() (bool, error) {
	return func() (bool, error) { return installed, err }
}

func bnPrecheckInputs() models.BlockNodeInputs {
	return models.BlockNodeInputs{Namespace: "block-node", Release: "block-node"}
}

func runPrecheckBlockNode(t *testing.T, fake *fakeCapsuleClient, force bool) *automa.Report {
	t.Helper()
	step, err := PrecheckBlockNode(bnPrecheckInputs(), force, fake.provider()).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func runPrecheckBlockNodeLegacy(t *testing.T, fake *fakeCapsuleClient, legacy func() (bool, error)) *automa.Report {
	t.Helper()
	step, err := PrecheckBlockNodeLegacy(bnPrecheckInputs(), fake.provider(), legacy).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

// --- PrecheckBlockNode (new CR-model install guard) ---

// No CR → install may proceed.
func TestPrecheckBlockNode_Absent(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	rpt := runPrecheckBlockNode(t, fake, false)
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

// CR exists, no --force → refuse.
func TestPrecheckBlockNode_CapsuleExistsNoForce(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{"block-node": "deployed"}}
	rpt := runPrecheckBlockNode(t, fake, false)
	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "already installed")
	assert.Contains(t, rpt.Error.Error(), "--force")
}

// CR exists, --force → re-apply path proceeds.
func TestPrecheckBlockNode_CapsuleExistsWithForce(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{"block-node": "deployed"}}
	rpt := runPrecheckBlockNode(t, fake, true)
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

// --- PrecheckBlockNodeLegacy (legacy-model guard) ---

// No CR, no legacy release → pass.
func TestPrecheckBlockNodeLegacy_NothingInstalled(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	rpt := runPrecheckBlockNodeLegacy(t, fake, legacyInstalledFn(false, nil))
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

// CR present → not a legacy release → pass (legacy check is skipped).
func TestPrecheckBlockNodeLegacy_CapsulePresent(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{"block-node": "deployed"}}
	rpt := runPrecheckBlockNodeLegacy(t, fake, legacyInstalledFn(true, nil))
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

// No CR but a legacy Helm-managed release exists → refuse and point at uninstall.
func TestPrecheckBlockNodeLegacy_LegacyReleaseBlocks(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	rpt := runPrecheckBlockNodeLegacy(t, fake, legacyInstalledFn(true, nil))
	assert.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "legacy Helm-managed block node")
}

// A legacy-check error surfaces as a failed precheck rather than a silent pass.
func TestPrecheckBlockNodeLegacy_CheckError(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	rpt := runPrecheckBlockNodeLegacy(t, fake, legacyInstalledFn(false, errorx.IllegalState.New("helm boom")))
	assert.Equal(t, automa.StatusFailed, rpt.Status)
}

// A nil legacy checker is tolerated: with no CR the precheck passes.
func TestPrecheckBlockNodeLegacy_NilChecker(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	rpt := runPrecheckBlockNodeLegacy(t, fake, nil)
	assert.Equal(t, automa.StatusSuccess, rpt.Status)
}

// splitBlockNodeChart derives the HelmCapsule repo + chart name from inputs.
func TestSplitBlockNodeChart(t *testing.T) {
	// Full OCI ref, no explicit ChartName: last segment is the chart name.
	repo, chart := splitBlockNodeChart(models.BlockNodeInputs{
		Chart: "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server",
	})
	assert.Equal(t, "oci://ghcr.io/hiero-ledger/hiero-block-node", repo)
	assert.Equal(t, "block-node-server", chart)

	// Explicit ChartName is authoritative; Chart is the bare repo.
	repo, chart = splitBlockNodeChart(models.BlockNodeInputs{
		Chart:     "oci://ghcr.io/hiero-ledger/hiero-block-node",
		ChartName: "block-node-server",
	})
	assert.Equal(t, "oci://ghcr.io/hiero-ledger/hiero-block-node", repo)
	assert.Equal(t, "block-node-server", chart)
}

// buildBlockNodeHelmCapsule sets the required HelmCapsule fields from inputs.
func TestBuildBlockNodeHelmCapsule(t *testing.T) {
	inputs := models.BlockNodeInputs{
		Namespace:    "block-node",
		Release:      "block-node",
		Chart:        "oci://ghcr.io/hiero-ledger/hiero-block-node",
		ChartName:    "block-node-server",
		ChartVersion: "0.44.2",
	}
	capsule, err := buildBlockNodeHelmCapsule(inputs, []byte("service:\n  type: LoadBalancer\n"))
	require.NoError(t, err)

	assert.Equal(t, kube.SoloOperatorGroup+"/"+kube.SoloOperatorVersion, capsule.APIVersion)
	assert.Equal(t, string(kube.KindHelmCapsule), capsule.Kind)
	assert.Equal(t, "block-node", capsule.Name)
	assert.Equal(t, "block-node", capsule.Namespace)
	require.NotNil(t, capsule.Spec.Component)
	assert.Equal(t, "block-node", string(*capsule.Spec.Component))
	require.NotNil(t, capsule.Spec.OrbitRef)
	assert.Equal(t, "block-node", capsule.Spec.OrbitRef.Name)
	assert.Equal(t, "oci://ghcr.io/hiero-ledger/hiero-block-node", capsule.Spec.Chart.Repo)
	assert.Equal(t, "block-node-server", capsule.Spec.Chart.Chart)
	assert.Equal(t, "0.44.2", capsule.Spec.Chart.Version)
	require.NotNil(t, capsule.Spec.Values)
	assert.JSONEq(t, `{"service":{"type":"LoadBalancer"}}`, string(capsule.Spec.Values.Raw))
}
