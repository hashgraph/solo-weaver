// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// UpgradeCiliumCLIWorkflow puts the cilium CLI on version.
func UpgradeCiliumCLIWorkflow(version string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("upgrade-cilium-cli").
		Steps(steps.UpgradeHostBinary("cilium", version, true, software.NewCiliumInstaller))
}

// CiliumHopWorkflow moves the running Cilium from one catalog version to the
// next. It stops at the first failed step and has no rollback: going back is a
// second `cilium upgrade`, which a person should decide on.
func CiliumHopWorkflow(from, to string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("cilium-hop").
		Steps(steps.CiliumHopSteps(from, to, kube.NewClient)...)
}
