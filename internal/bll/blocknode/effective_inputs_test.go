// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"context"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// fakeBlockNodeChecker is a no-op reality.Checker[state.BlockNodeState] that
// returns a fixed (not-deployed) state, so effective-input resolution takes the
// user-supplied values.
type fakeBlockNodeChecker struct{ st state.BlockNodeState }

func (f *fakeBlockNodeChecker) RefreshState(_ context.Context) (state.BlockNodeState, error) {
	return f.st, nil
}

// TestResolveEffectiveInputs_CarriesTimeout is a regression guard for #912: the
// --timeout value must survive resolveBlocknodeEffectiveInputs, which rebuilds
// BlockNodeInputs field-by-field. A field left out of that literal is silently
// dropped between CLI validation and the workflow, so the helm call falls back
// to the 5m default (which is exactly what happened before this fix).
func TestResolveEffectiveInputs_CarriesTimeout(t *testing.T) {
	checker := &fakeBlockNodeChecker{st: state.NewBlockNodeState()}
	r, err := rsl.NewBlockNodeRuntimeResolver(models.Config{}, state.NewBlockNodeState(), checker, 10*time.Minute)
	require.NoError(t, err)
	runtime := r.(*rsl.BlockNodeRuntimeResolver)

	inputs := models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:    "block-node",
			Release:      "block-node",
			Chart:        "oci://example.com/block-node",
			ChartVersion: "0.37.1",
			Storage:      models.BlockNodeStorage{BasePath: "/mnt/fast-storage"},
			Timeout:      15 * time.Minute,
		},
	}

	eff, err := resolveBlocknodeEffectiveInputs(
		runtime,
		models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode},
		inputs,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 15*time.Minute, eff.Custom.Timeout,
		"--timeout must be carried through effective-input resolution")
}

// baseTcInputs returns valid inputs with the resolvable fields set but the
// traffic-shaping content deliberately left empty, so the state fallback governs.
func baseTcInputs() models.UserInputs[models.BlockNodeInputs] {
	return models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:    "block-node",
			Release:      "block-node",
			Chart:        "oci://example.com/block-node",
			ChartVersion: "0.37.1",
			Storage:      models.BlockNodeStorage{BasePath: "/mnt/fast-storage"},
		},
	}
}

// deployedShapingBlockNodeState returns a deployed BlockNodeState so that
// resolveBlocknodeEffectiveInputs can resolve an upgrade (chart-version
// resolution requires a StrategyState/Reality source, i.e. a deployed release).
// BasePath is set so the storage resolver's deployed-state validation passes.
func deployedShapingBlockNodeState() state.BlockNodeState {
	st := state.NewBlockNodeState()
	st.ReleaseInfo = state.HelmReleaseInfo{
		Status:       release.StatusDeployed,
		Name:         "block-node",
		Namespace:    "block-node",
		ChartName:    "block-node",
		ChartRef:     "oci://example.com/block-node",
		ChartVersion: "0.37.1",
	}
	st.Storage = models.BlockNodeStorage{BasePath: "/mnt/fast-storage"}
	return st
}

// TestResolveEffectiveInputs_TrafficShapingFallbackFromState verifies that when
// the operator supplies no egress NIC / rate / overrides (the upgrade case, which
// has no such flags), the persisted BlockNodeState.Shaping is used so the tc steps
// re-assert the original shaping instead of auto-detecting (issue #932, AC3).
func TestResolveEffectiveInputs_TrafficShapingFallbackFromState(t *testing.T) {
	prio := 0
	persisted := deployedShapingBlockNodeState()
	persisted.Shaping = &state.ShapingState{
		EgressInterface: "eth0",
		LinkRate:        "1gbit",
		ShapeOverrides: map[string]models.ShapeOverride{
			"publisher": {Rate: "800mbit", Ceil: "1gbit", Prio: &prio},
		},
	}

	checker := &fakeBlockNodeChecker{st: state.NewBlockNodeState()}
	r, err := rsl.NewBlockNodeRuntimeResolver(models.Config{}, persisted, checker, 10*time.Minute)
	require.NoError(t, err)
	runtime := r.(*rsl.BlockNodeRuntimeResolver)

	eff, err := resolveBlocknodeEffectiveInputs(
		runtime,
		models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlockNode},
		baseTcInputs(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "eth0", eff.Custom.EgressInterface, "egress NIC must fall back to persisted Shaping")
	assert.Equal(t, "1gbit", eff.Custom.LinkRate, "link rate must fall back to persisted Shaping")
	require.Contains(t, eff.Custom.ShapeOverrides, "publisher")
	assert.Equal(t, "800mbit", eff.Custom.ShapeOverrides["publisher"].Rate)
}

// TestResolveEffectiveInputs_ExplicitTrafficShapingWins verifies that an explicit
// operator-supplied value beats the persisted state fallback (issue #932, AC4).
func TestResolveEffectiveInputs_ExplicitTrafficShapingWins(t *testing.T) {
	persisted := state.NewBlockNodeState()
	persisted.Shaping = &state.ShapingState{EgressInterface: "eth0", LinkRate: "1gbit"}

	checker := &fakeBlockNodeChecker{st: state.NewBlockNodeState()}
	r, err := rsl.NewBlockNodeRuntimeResolver(models.Config{}, persisted, checker, 10*time.Minute)
	require.NoError(t, err)
	runtime := r.(*rsl.BlockNodeRuntimeResolver)

	inputs := baseTcInputs()
	inputs.Custom.EgressInterface = "ens5"
	inputs.Custom.LinkRate = "10gbit"

	eff, err := resolveBlocknodeEffectiveInputs(
		runtime,
		models.Intent{Action: models.ActionReconfigure, Target: models.TargetBlockNode},
		inputs,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "ens5", eff.Custom.EgressInterface, "explicit egress NIC must win over persisted Shaping")
	assert.Equal(t, "10gbit", eff.Custom.LinkRate, "explicit link rate must win over persisted Shaping")
}
