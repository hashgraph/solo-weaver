// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// CiliumHopWorkflow moves the running Cilium from one catalog version to the
// next. When cli is set it first puts the cilium CLI on that version. It stops
// at the first failed step and has no rollback: going back is a second
// `cilium upgrade`, which a person should decide on.
func CiliumHopWorkflow(from, to, cli string) *automa.WorkflowBuilder {
	var all []automa.Builder
	if cli != "" {
		all = append(all, steps.UpgradeHostBinary("cilium", cli, true, software.NewCiliumInstaller))
	}
	all = append(all, steps.CiliumHopSteps(from, to, kube.NewClient)...)
	return automa.NewWorkflowBuilder().WithId("cilium-hop").Steps(all...)
}
