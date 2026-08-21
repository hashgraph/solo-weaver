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

// TestResolveEffectiveInputs_CarriesStatusz is a regression guard (same class as
// the #912 timeout drop): the operator-supplied statusz overrides must survive
// resolveBlocknodeEffectiveInputs, which rebuilds BlockNodeInputs field-by-field.
// A field omitted from that literal is silently dropped between CLI validation and
// the daemon-config step, so daemon.yaml would never receive the operator's value.
func TestResolveEffectiveInputs_CarriesStatusz(t *testing.T) {
	checker := &fakeBlockNodeChecker{st: state.NewBlockNodeState()}
	r, err := rsl.NewBlockNodeRuntimeResolver(models.Config{}, state.NewBlockNodeState(), checker, 10*time.Minute)
	require.NoError(t, err)
	runtime := r.(*rsl.BlockNodeRuntimeResolver)

	inputs := models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:           "block-node",
			Release:             "block-node",
			Chart:               "oci://example.com/block-node",
			ChartVersion:        "0.37.1",
			Storage:             models.BlockNodeStorage{BasePath: "/mnt/fast-storage"},
			StatuszBaseURL:      "http://127.0.0.1:8080",
			StatuszPollInterval: "5s",
		},
	}

	eff, err := resolveBlocknodeEffectiveInputs(
		runtime,
		models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode},
		inputs,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8080", eff.Custom.StatuszBaseURL,
		"--statusz-base-url must be carried through effective-input resolution")
	assert.Equal(t, "5s", eff.Custom.StatuszPollInterval,
		"--statusz-poll-interval must be carried through effective-input resolution")
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
// the operator supplies no egress NIC / rate (the upgrade case, which has no such
// flags), the persisted BlockNodeState.Shaping is used so the tc steps re-assert
// the original egress device and trunk rate instead of auto-detecting
// (issue #932, AC3).
//
// ShapeOverrides is deliberately NOT backfilled: re-asserting an install-time
// --shape would overwrite a later `network shape set` on the same class, which is
// the clobber #1037 fixes. Per-class values live in the shape registry, which the
// tc steps now preserve across a re-provision at an unchanged trunk rate.
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
	assert.Empty(t, eff.Custom.ShapeOverrides,
		"persisted --shape must NOT be re-asserted; it would clobber later `network shape set` tuning (#1037)")
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
	inputs.Custom.ShapeOverrides = map[string]models.ShapeOverride{
		"partner": {Rate: "300mbit"},
	}

	eff, err := resolveBlocknodeEffectiveInputs(
		runtime,
		models.Intent{Action: models.ActionReconfigure, Target: models.TargetBlockNode},
		inputs,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "ens5", eff.Custom.EgressInterface, "explicit egress NIC must win over persisted Shaping")
	assert.Equal(t, "10gbit", eff.Custom.LinkRate, "explicit link rate must win over persisted Shaping")
	require.Contains(t, eff.Custom.ShapeOverrides, "partner",
		"--shape supplied on this run must reach the tc steps")
	assert.Equal(t, "300mbit", eff.Custom.ShapeOverrides["partner"].Rate)
}
