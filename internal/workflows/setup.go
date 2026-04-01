// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// NodeSetupWorkflow creates a comprehensive setup workflow for any node type
// It runs preflight checks first, then performs the actual setup.
func NodeSetupWorkflow(nodeType string, profile string, skipHardwareChecks bool) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId(nodeType+"-node-setup").
		Steps(
			// First run preflight checks to ensure system readiness
			NewNodeSafetyCheckWorkflow(nodeType, profile, skipHardwareChecks),
			// Then perform the actual system setup (packages, kernel modules, etc.)
			systemSetupWorkflow(nodeType),
		)
}

// systemSetupWorkflow installs system-level dependencies, kernel modules, and
// services required before Kubernetes can be set up. Rendered as the
// "System Setup" phase in the TUI.
func systemSetupWorkflow(nodeType string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId(nodeType+"-system-setup").
		Steps(
			steps.SetupHomeDirectoryStructure(models.Paths()),
			steps.RefreshSystemPackageIndex(),
			steps.InstallSystemPackage("iptables", software.NewIptables),
			steps.InstallSystemPackage("gpg", software.NewGpg),
			steps.InstallSystemPackage("conntrack", software.NewConntrack),
			steps.InstallSystemPackage("ebtables", software.NewEbtables),
			steps.InstallSystemPackage("socat", software.NewSocat),
			steps.InstallSystemPackage("nftables", software.NewNftables),
			steps.SetupSystemdService("nftables"),
			steps.InstallKernelModule("overlay"),
			steps.InstallKernelModule("br_netfilter"),
			steps.AutoRemoveOrphanedPackages(),
			//steps.RemoveSystemPackage("containerd", software.NewContainerd),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "System Setup")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "System Setup")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "System Setup")
		})
}
