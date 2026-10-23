// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/bll"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func newMinimalUninstallHandler() *UninstallHandler {
	return &UninstallHandler{BaseHandler: bll.BaseHandler[models.ConsensusNodeInputs]{}}
}

func uninstallInputs() models.UserInputs[models.ConsensusNodeInputs] {
	return models.UserInputs[models.ConsensusNodeInputs]{
		Custom: models.ConsensusNodeInputs{Namespace: "hiero-network-1", NodeId: 2},
	}
}

// flattenStepIDs lists the leaf step IDs of a workflow, in order, descending into phases.
func flattenStepIDs(t *testing.T, stp automa.Step) []string {
	t.Helper()
	wf, ok := stp.(automa.Workflow)
	if !ok {
		return []string{stp.Id()}
	}
	var ids []string
	for _, s := range wf.Steps() {
		ids = append(ids, flattenStepIDs(t, s)...)
	}
	return ids
}

func TestUninstall_PrepareEffectiveInputs_OrbitIsNamespace(t *testing.T) {
	h := newMinimalUninstallHandler()

	eff, err := h.PrepareEffectiveInputs(models.Intent{}, uninstallInputs())

	require.NoError(t, err)
	assert.Equal(t, "hiero-network-1", eff.Custom.OrbitName)
	assert.Equal(t, int64(2), eff.Custom.NodeId)
}

// The PVCs must be released before the capsule delete, or garbage collection deletes them.
// The capsule must go before the config CRs it references, and before the Orbit check.
func TestUninstall_BuildWorkflow_StepOrder(t *testing.T) {
	h := newMinimalUninstallHandler()
	eff, err := h.PrepareEffectiveInputs(models.Intent{}, uninstallInputs())
	require.NoError(t, err)

	wb, err := h.BuildWorkflow(state.State{}, *eff)
	require.NoError(t, err)
	assert.Equal(t, "consensus-node-uninstall", wb.Id())

	wf, err := wb.Build()
	require.NoError(t, err)
	assert.Equal(t, []string{
		steps.ReleaseConsensusPVCsStepId,
		steps.DeleteConsensusCapsuleStepId,
		steps.DeleteConsensusConfigCRsStepId,
		steps.DeleteOrbitIfLastStepId,
	}, flattenStepIDs(t, wf))
}

func TestForAction_Uninstall(t *testing.T) {
	uninstall := newMinimalUninstallHandler()
	h := &Handlers{uninstall: uninstall}

	got, err := h.ForAction(models.ActionUninstall)

	require.NoError(t, err)
	assert.Same(t, uninstall, got)
}

// fakeExistsClient answers ResourceExists from a set of names; other methods are unused.
type fakeExistsClient struct {
	steps.CapsuleKubeClient
	existing  map[string]bool
	existsErr error
}

func (f *fakeExistsClient) ResourceExists(_ context.Context, _, _, _, name string) (bool, error) {
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return f.existing[name], nil
}

func providerFor(kc steps.CapsuleKubeClient) steps.CapsuleKubeProvider {
	return func(context.Context) (steps.CapsuleKubeClient, error) { return kc, nil }
}

// stateWithNodes holds node 2 (the one uninstalled) and node 3 on the same orbit.
func stateWithNodes() *state.State {
	s := &state.State{}
	s.ConsensusNodes = map[string]state.ConsensusNodeState{
		models.ConsensusNodeStateKey("hiero-network-1", 2): {Namespace: "hiero-network-1", NodeId: 2},
		models.ConsensusNodeStateKey("hiero-network-1", 3): {Namespace: "hiero-network-1", NodeId: 3},
	}
	s.ConsensusOrbits = map[string]state.ConsensusOrbitState{"hiero-network-1": {LedgerId: "0x01"}}
	return s
}

func runPrune(t *testing.T, provider steps.CapsuleKubeProvider) *state.State {
	t.Helper()
	h := newMinimalUninstallHandler()
	eff, err := h.PrepareEffectiveInputs(models.Intent{}, uninstallInputs())
	require.NoError(t, err)

	full := stateWithNodes()
	require.NoError(t, pruneUninstalledConsensusState(context.Background(), provider)(full, *eff))
	return full
}

var (
	node2Key = models.ConsensusNodeStateKey("hiero-network-1", 2)
	node3Key = models.ConsensusNodeStateKey("hiero-network-1", 3)
	capsule2 = models.ConsensusCapsuleName("hiero-network-1", 2)
)

func TestPrune_CapsuleAndOrbitGone_RemovesBoth(t *testing.T) {
	full := runPrune(t, providerFor(&fakeExistsClient{existing: map[string]bool{}}))

	assert.NotContains(t, full.ConsensusNodes, node2Key)
	assert.Contains(t, full.ConsensusNodes, node3Key, "other nodes must stay")
	assert.NotContains(t, full.ConsensusOrbits, "hiero-network-1")
}

func TestPrune_OrbitStillLive_KeepsOrbit(t *testing.T) {
	full := runPrune(t, providerFor(&fakeExistsClient{existing: map[string]bool{"hiero-network-1": true}}))

	assert.NotContains(t, full.ConsensusNodes, node2Key)
	assert.Contains(t, full.ConsensusOrbits, "hiero-network-1")
}

// A failed uninstall leaves the capsule in place; the node must stay in state.
func TestPrune_CapsuleStillLive_KeepsNode(t *testing.T) {
	full := runPrune(t, providerFor(&fakeExistsClient{existing: map[string]bool{capsule2: true, "hiero-network-1": true}}))

	assert.Contains(t, full.ConsensusNodes, node2Key)
	assert.Contains(t, full.ConsensusOrbits, "hiero-network-1")
}

func TestPrune_ClusterError_KeepsState(t *testing.T) {
	full := runPrune(t, providerFor(&fakeExistsClient{existsErr: errors.New("connection refused")}))

	assert.Contains(t, full.ConsensusNodes, node2Key)
	assert.Contains(t, full.ConsensusOrbits, "hiero-network-1")
}

func TestPrune_ProviderError_KeepsState(t *testing.T) {
	full := runPrune(t, func(context.Context) (steps.CapsuleKubeClient, error) {
		return nil, errors.New("no kubeconfig")
	})

	assert.Contains(t, full.ConsensusNodes, node2Key)
}

// Deleting the CRDs deletes every CR, so the node and Orbit leave state too.
func TestPrune_CRDsAbsent_RemovesBoth(t *testing.T) {
	noMatch := errorx.IllegalArgument.Wrap(&meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: kube.SoloOperatorGroup}}, "mapping")
	full := runPrune(t, providerFor(&fakeExistsClient{existsErr: noMatch}))

	assert.NotContains(t, full.ConsensusNodes, node2Key)
	assert.NotContains(t, full.ConsensusOrbits, "hiero-network-1")
}
