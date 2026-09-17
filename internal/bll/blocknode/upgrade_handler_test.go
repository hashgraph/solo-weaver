// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

func newMinimalUpgradeHandler() *UpgradeHandler {
	return &UpgradeHandler{}
}

// deployedStateForUpgrade returns a deployed block-node state at chart version
// 0.30.0 with the given traffic-shaping-disabled decision recorded.
func deployedStateForUpgrade(trafficShapingDisabled bool) state.State {
	return state.State{
		StateRecord: state.StateRecord{
			BlockNodeState: state.BlockNodeState{
				ReleaseInfo: state.HelmReleaseInfo{
					Status:       release.StatusDeployed,
					ChartRef:     "oci://example.com/block-node",
					ChartVersion: "0.30.0",
				},
				Storage:                models.BlockNodeStorage{BasePath: "/mnt/storage"},
				TrafficShapingDisabled: trafficShapingDisabled,
			},
		},
	}
}

func upgradeInputs() models.UserInputs[models.BlockNodeInputs] {
	return models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:    "block-node-ns",
			Release:      "block-node",
			Chart:        "oci://example.com/block-node",
			ChartVersion: "0.31.0",
			Storage:      models.BlockNodeStorage{BasePath: "/mnt/storage"},
			ReuseValues:  true,
		},
	}
}

// TestUpgrade_TrafficShapingEnabled_ReAssertsPlane verifies that an upgrade of a
// block node that had traffic shaping enabled at install re-asserts the full
// network plane (create-if-missing) ahead of the chart upgrade.
func TestUpgrade_TrafficShapingEnabled_ReAssertsPlane(t *testing.T) {
	h := newMinimalUpgradeHandler()

	wb, err := h.BuildWorkflow(deployedStateForUpgrade(false), upgradeInputs())
	require.NoError(t, err)

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, []string{
		steps.NetworkFirewallCreateStepId,
		steps.NetworkPolicyCreateStepId,
		steps.NftWeaverPersistStepId,
		steps.TcEgressPersistStepId,
		steps.TcIngressRecordStepId,
		steps.BlockNodeDaemonConfigStepId,
		steps.RestartDaemonServiceStepId,
		steps.UpgradeBlockNodeStepId,
	}, ids)
}

// TestUpgrade_TrafficShapingDisabled_NoTeardown verifies that upgrading a block
// node that had traffic shaping disabled at install re-asserts only the
// self-gating host firewall and never emits teardown steps — a routine version
// bump must not disarm anything.
func TestUpgrade_TrafficShapingDisabled_NoTeardown(t *testing.T) {
	h := newMinimalUpgradeHandler()

	wb, err := h.BuildWorkflow(deployedStateForUpgrade(true), upgradeInputs())
	require.NoError(t, err)

	ids := workflowStepIDs(t, wb)
	assert.Equal(t, []string{
		steps.NetworkFirewallCreateStepId,
		steps.UpgradeBlockNodeStepId,
	}, ids)
}

// TestUpgrade_LeaveScaledDown_TrailingScaleDown pins --no-scale-up on both
// upgrade branches. The scale-down has to follow the chart upgrade, which
// re-asserts the chart's replica default.
func TestUpgrade_LeaveScaledDown_TrailingScaleDown(t *testing.T) {
	h := newMinimalUpgradeHandler()

	for _, tc := range []struct {
		name         string
		resetStorage bool
		want         []string
	}{
		{
			name:         "plain_upgrade",
			resetStorage: false,
			want: []string{
				steps.NetworkFirewallCreateStepId,
				steps.UpgradeBlockNodeStepId,
				steps.ScaleDownAfterUpgradeStepId,
			},
		},
		{
			name:         "with_reset",
			resetStorage: true,
			want: []string{
				steps.NetworkFirewallCreateStepId,
				steps.PurgeBlockNodeStorageStepId,
				steps.UpgradeBlockNodeStepId,
				steps.ScaleDownAfterUpgradeStepId,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := upgradeInputs()
			inputs.Custom.ResetStorage = tc.resetStorage
			inputs.Custom.LeaveScaledDown = true

			// Traffic shaping disabled keeps the network prefix to the single
			// self-gating firewall step, so the assertion stays about the tail.
			wb, err := h.BuildWorkflow(deployedStateForUpgrade(true), inputs)
			require.NoError(t, err)

			assert.Equal(t, tc.want, workflowStepIDs(t, wb))
		})
	}
}
