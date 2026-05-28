// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NewDaemonServiceInstallWorkflow installs the solo-provisioner-daemon systemd service unit file
// and enables it. Requires root privileges.
func NewDaemonServiceInstallWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("daemon-service-install-workflow").Steps(
		CheckPrivilegesStep(),
		steps.InstallDaemonServiceStep(),
	)
}

// NewDaemonServiceUninstallWorkflow disables and removes the solo-provisioner-daemon systemd
// service unit file. Requires root privileges.
func NewDaemonServiceUninstallWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("daemon-service-uninstall-workflow").Steps(
		CheckPrivilegesStep(),
		steps.RemoveDaemonServiceStep(),
	)
}

// NewDaemonServiceCheckWorkflow checks the health of the daemon installation:
// unit file, service enabled/running, binary, sudoers entry, and Unix socket health.
func NewDaemonServiceCheckWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("daemon-service-check-workflow").Steps(
		steps.CheckDaemonServiceStep(models.Paths().DaemonSockPath),
	)
}
