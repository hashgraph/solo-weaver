// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package workflows

import (
	"testing"

	daemon "github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPaths() models.WeaverPaths {
	return models.WeaverPaths{
		DaemonCNKubeconfigPath: "/opt/solo/weaver/config/daemon-cn.kubeconfig",
		DaemonBNKubeconfigPath: "/opt/solo/weaver/config/daemon-bn.kubeconfig",
	}
}

func TestBuildComponentSpecs_BlockNodeTrafficShaper(t *testing.T) {
	cfg := daemon.DaemonConfig{Components: daemon.DaemonComponents{
		BlockNode: &daemon.BlockNodeComponentConfig{
			Enabled:  true,
			Orbit:    "hedera-block-node",
			Monitors: daemon.BlockNodeMonitors{TrafficShaper: true},
		},
	}}

	specs := buildComponentSpecs(cfg, testPaths())
	require.Len(t, specs, 1)

	bn := specs[0]
	assert.Equal(t, "bn", bn.ShortName)
	assert.Equal(t, "hedera-block-node", bn.Namespace)
	assert.Equal(t, "/opt/solo/weaver/config/daemon-bn.kubeconfig", bn.KubeconfigPath)

	// Exactly the two least-privilege rules: pods get/list/watch + pods/exec create.
	require.Len(t, bn.PolicyRules, 2)
	assert.Equal(t, []string{"pods"}, bn.PolicyRules[0].Resources)
	assert.ElementsMatch(t, []string{"get", "list", "watch"}, bn.PolicyRules[0].Verbs)
	assert.Equal(t, []string{"pods/exec"}, bn.PolicyRules[1].Resources)
	assert.ElementsMatch(t, []string{"create"}, bn.PolicyRules[1].Verbs)
}

func TestBuildComponentSpecs_BlockNodeWithoutTrafficShaperNoSpec(t *testing.T) {
	cfg := daemon.DaemonConfig{Components: daemon.DaemonComponents{
		BlockNode: &daemon.BlockNodeComponentConfig{
			Enabled:  true,
			Orbit:    "hedera-block-node",
			Monitors: daemon.BlockNodeMonitors{TrafficShaper: false},
		},
	}}
	assert.Empty(t, buildComponentSpecs(cfg, testPaths()),
		"block-node with the traffic-shaper monitor off needs no K8s RBAC")
}

func TestBuildComponentSpecs_NoBlockNodeNoSpec(t *testing.T) {
	assert.Empty(t, buildComponentSpecs(daemon.DaemonConfig{}, testPaths()))
}

func TestBuildComponentSpecs_ConsensusAndBlockNode(t *testing.T) {
	cfg := daemon.DaemonConfig{Components: daemon.DaemonComponents{
		ConsensusNode: &daemon.ConsensusNodeComponentConfig{
			Enabled:  true,
			Orbit:    "hedera-network",
			Monitors: daemon.ConsensusNodeMonitors{Upgrade: true},
		},
		BlockNode: &daemon.BlockNodeComponentConfig{
			Enabled:  true,
			Orbit:    "hedera-block-node",
			Monitors: daemon.BlockNodeMonitors{TrafficShaper: true},
		},
	}}

	specs := buildComponentSpecs(cfg, testPaths())
	require.Len(t, specs, 2)
	shorts := []string{specs[0].ShortName, specs[1].ShortName}
	assert.ElementsMatch(t, []string{"cn", "bn"}, shorts)
}
