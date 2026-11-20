// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// CiliumHopWorkflow moves the running Cilium from one catalog version to the
// next with the cilium CLI at cli, which it first installs when the host has
// another version. It stops at the first failed step and has no rollback:
// going back is a second `cilium upgrade`, which a person should decide on.
func CiliumHopWorkflow(from, to, cli string) *automa.WorkflowBuilder {
	all := []automa.Builder{steps.UpgradeHostBinary(software.CiliumBinaryName, cli, true, software.NewCiliumInstaller)}
	all = append(all, steps.CiliumHopSteps(from, to, cli, kube.NewClient)...)
	return automa.NewWorkflowBuilder().WithId("cilium-hop").Steps(all...)
}
