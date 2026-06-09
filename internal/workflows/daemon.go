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
// Used by uninstall (which always reads from the existing on-disk config).
func loadOrbit(paths models.WeaverPaths) (string, error) {
	cfg, err := daemon.LoadDaemonConfig(paths.DaemonConfigPath)
	if err != nil {
		return "", err
	}
	if cfg.Components.ConsensusNode == nil || cfg.Components.ConsensusNode.Orbit == "" {
		return "", errorx.IllegalState.New("daemon config %s: components.consensus_node.orbit must not be empty", paths.DaemonConfigPath)
	}
	return cfg.Components.ConsensusNode.Orbit, nil
}

// NewDaemonServiceInstallWorkflow provisions the full daemon stack. The step
// list is built dynamically from cfg so that only the preflight and RBAC steps
// relevant to the enabled components are included:
//
//  1. Check root privileges                         (always)
//  2. Install the daemon binary                     (always)
//  3. Ensure CN upgrade directory exists            (consensus_node only)
//  4. Check K8s cluster is reachable                (any K8s-dependent component)
//  5. Create RBAC + write kubeconfig per component  (per enabled K8s-dependent component)
//  6. Install + enable + start systemd service unit (always)
//
// The caller is responsible for ensuring daemon.yaml exists at
// paths.DaemonConfigPath before calling this function.
func NewDaemonServiceInstallWorkflow(cfg daemon.DaemonConfig, daemonSrc steps.DaemonBinarySource) (*automa.WorkflowBuilder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "invalid daemon config")
	}

	paths := models.Paths()

	wfSteps := []automa.Builder{
		CheckPrivilegesStep(),
		steps.InstallDaemonBinaryStep(daemonSrc, paths),
	}

	// consensus_node: CN-specific preflight + RBAC
	if cn := cfg.Components.ConsensusNode; cn != nil && cn.Enabled {
		wfSteps = append(wfSteps, steps.EnsureDaemonHgcAppDirStep(cn.EffectiveUpgradeDir()))
		wfSteps = append(wfSteps, steps.CheckClusterStep())
		wfSteps = append(wfSteps, steps.CreateConsensusNodeRBACStep(cn.Orbit))
		wfSteps = append(wfSteps, steps.WriteConsensusNodeKubeconfigStep(paths, cn.Orbit))
	}

	wfSteps = append(wfSteps, steps.InstallDaemonServiceStep(paths))

	return automa.NewWorkflowBuilder().WithId("daemon-service-install-workflow").Steps(wfSteps...), nil
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
		steps.RemoveConsensusNodeKubeconfigStep(paths),
		steps.DeleteConsensusNodeRBACStep(orbit),
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
