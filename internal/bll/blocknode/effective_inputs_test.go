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
