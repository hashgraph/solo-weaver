// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	daemon "github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// loadOrbit reads the orbit namespace from daemon.yaml. It fails fast if the
// config is missing or invalid — provisioning cannot proceed without it.
func loadOrbit(paths models.WeaverPaths) (string, error) {
	cfg, err := daemon.LoadDaemonConfig(paths.DaemonConfigPath)
	if err != nil {
		return "", err
	}
	if cfg.Orbit == "" {
		return "", errorx.IllegalState.New("daemon config %s: orbit namespace must not be empty", paths.DaemonConfigPath)
	}
	return cfg.Orbit, nil
}

// NewDaemonServiceInstallWorkflow provisions the full daemon stack:
//  1. Check root privileges
//  2. Check K8s cluster is reachable
//  3. Create RBAC (SA + ClusterRole + CRB + token Secret)
//  4. Write daemon kubeconfig
//  5. Install + enable + start systemd service unit
func NewDaemonServiceInstallWorkflow() (*automa.WorkflowBuilder, error) {
	paths := models.Paths()
	orbit, err := loadOrbit(paths)
	if err != nil {
		return nil, err
	}
	return automa.NewWorkflowBuilder().WithId("daemon-service-install-workflow").Steps(
		CheckPrivilegesStep(),
		steps.CheckClusterStep(),
		steps.CreateDaemonRBACStep(orbit),
		steps.WriteDaemonKubeconfigStep(paths, orbit),
		steps.InstallDaemonServiceStep(paths),
	), nil
}

// NewDaemonServiceUninstallWorkflow tears down the daemon stack in reverse:
//  1. Check root privileges
//  2. Stop + disable + remove systemd service unit
//  3. Remove daemon kubeconfig
//  4. Delete RBAC (CRB + CR + Secret + SA)
func NewDaemonServiceUninstallWorkflow() (*automa.WorkflowBuilder, error) {
	paths := models.Paths()
	orbit, err := loadOrbit(paths)
	if err != nil {
		return nil, err
	}
	return automa.NewWorkflowBuilder().WithId("daemon-service-uninstall-workflow").Steps(
		CheckPrivilegesStep(),
		steps.RemoveDaemonServiceStep(paths),
		steps.RemoveDaemonKubeconfigStep(paths),
		steps.DeleteDaemonRBACStep(orbit),
	), nil
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
