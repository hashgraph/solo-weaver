// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

func CheckWeaverInstallationWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("check-weaver-installation-workflow").Steps(
		steps.CheckWeaverInstallation(models.Paths().BinDir),
	)
}

func NewSelfInstallWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("self-install-workflow").Steps(
		CheckPrivilegesStep(),
		steps.EnsureWeaverOwnerStep(),
		steps.SetupHomeDirectoryStructure(models.Paths()),
		steps.InstallWeaver(models.Paths().BinDir),
		steps.InstallColocatedDaemonBinary(models.Paths().BinDir),
		steps.InstallSudoersStep(),
	)
}

// NewSelfUninstallWorkflow removes everything self-install and the daemon
// installer put on this host: the CLI, the daemon binary and its service, the
// network boot units, and the config tree under /etc.
//
// Order matters twice. The cluster check comes first so a refusal costs nothing.
// RemoveNetworkConfig runs before NftServiceTeardown because that step keeps the
// shared unit while the host-firewall .nft file is present.
//
// Live kernel state (loaded nft tables, tc qdiscs) is out of scope; it does not
// survive a reboot once the boot-replay inputs above are gone.
func NewSelfUninstallWorkflow() *automa.WorkflowBuilder {
	paths := models.Paths()
	return automa.NewWorkflowBuilder().WithId("self-uninstall-workflow").Steps(
		CheckPrivilegesStep(),
		steps.CheckNoProvisionedCluster(),
		steps.RemoveDaemonServiceStep(paths),
		steps.RemoveDaemonBinaryStep(paths),
		steps.RemoveNetworkConfig(),
		steps.NftServiceTeardown(),
		steps.TcEgressServiceTeardown(),
		steps.RemoveSudoersStep(),
		steps.UninstallWeaver(paths.BinDir),
	)
}
