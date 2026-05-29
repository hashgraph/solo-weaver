// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// NewDaemonServiceInstallWorkflow installs the solo-provisioner-daemon systemd service unit file
// into the weaver sandbox, creates a system symlink, enables, and starts the service.
// Requires root privileges.
func NewDaemonServiceInstallWorkflow() *automa.WorkflowBuilder {
	paths := models.Paths()
	return automa.NewWorkflowBuilder().WithId("daemon-service-install-workflow").Steps(
		CheckPrivilegesStep(),
		steps.InstallDaemonServiceStep(paths),
	)
}

// NewDaemonServiceUninstallWorkflow stops, disables, and removes the
// solo-provisioner-daemon systemd service — both the system symlink and the
// sandbox unit file. Requires root privileges.
func NewDaemonServiceUninstallWorkflow() *automa.WorkflowBuilder {
	paths := models.Paths()
	return automa.NewWorkflowBuilder().WithId("daemon-service-uninstall-workflow").Steps(
		CheckPrivilegesStep(),
		steps.RemoveDaemonServiceStep(paths),
	)
}

// NewDaemonServiceCheckWorkflow checks the health of the daemon installation:
// sandbox unit file, system symlink, service enabled/running, binary, sudoers entry,
// and Unix socket health.
func NewDaemonServiceCheckWorkflow() *automa.WorkflowBuilder {
	paths := models.Paths()
	return automa.NewWorkflowBuilder().WithId("daemon-service-check-workflow").Steps(
		steps.CheckDaemonServiceStep(paths, paths.DaemonSockPath),
	)
}
