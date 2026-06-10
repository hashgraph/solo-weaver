// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	daemon "github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	rbacv1 "k8s.io/api/rbac/v1"
)

// buildComponentSpecs constructs the slice of DaemonComponentSpec for all
// K8s-dependent components that are enabled in cfg. Host-only components
// (those that need no K8s RBAC or kubeconfig) are excluded.
//
// Adding a new K8s-dependent component (e.g. block-node) requires only a new
// branch here — no changes to the step implementations are needed.
func buildComponentSpecs(cfg daemon.DaemonConfig, paths models.WeaverPaths) []steps.DaemonComponentSpec {
	var specs []steps.DaemonComponentSpec

	if cn := cfg.Components.ConsensusNode; cn != nil && cn.Enabled {
		specs = append(specs, steps.DaemonComponentSpec{
			ShortName:      "cn",
			Namespace:      cn.Orbit,
			KubeconfigPath: paths.DaemonCNKubeconfigPath,
			PolicyRules: []rbacv1.PolicyRule{{
				APIGroups: []string{"hedera.com"},
				Resources: []string{"networkupgradeexecutes"},
				Verbs:     []string{"list", "watch"},
			}},
		})
	}

	// block_node: added in S7 (#667). When enabled, append:
	//   specs = append(specs, steps.DaemonComponentSpec{
	//       ShortName:      "bn",
	//       Namespace:      bn.Orbit,
	//       KubeconfigPath: paths.DaemonBNKubeconfigPath,
	//       PolicyRules:    <bn RBAC rules>,
	//   })

	return specs
}

// loadComponentSpecs reads daemon.yaml from paths and rebuilds the component
// spec slice. Used by uninstall which must derive specs from the on-disk config
// rather than a caller-supplied config.
func loadComponentSpecs(paths models.WeaverPaths) ([]steps.DaemonComponentSpec, error) {
	cfg, err := daemon.LoadDaemonConfig(paths.DaemonConfigPath)
	if err != nil {
		return nil, err
	}
	return buildComponentSpecs(cfg, paths), nil
}

// NewDaemonServiceInstallWorkflow provisions the full daemon stack. The step
// list is built dynamically from cfg so that only the preflight and RBAC steps
// relevant to the enabled components are included:
//
//  1. Check root privileges                           (always)
//  2. Install the daemon binary                       (always)
//  3. Ensure CN upgrade directory exists              (consensus_node only)
//  4. Check K8s cluster is reachable                  (any K8s-dependent component)
//  5. Create RBAC resources for all K8s components    (any K8s-dependent component)
//  6. Write per-component kubeconfigs                 (any K8s-dependent component)
//  7. Install + enable + start systemd service unit   (always)
//
// The caller is responsible for ensuring daemon.yaml exists at
// paths.DaemonConfigPath before calling this function.
func NewDaemonServiceInstallWorkflow(cfg daemon.DaemonConfig, daemonSrc steps.DaemonBinarySource) (*automa.WorkflowBuilder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "invalid daemon config")
	}

	paths := models.Paths()
	componentSpecs := buildComponentSpecs(cfg, paths)

	wfSteps := []automa.Builder{
		CheckPrivilegesStep(),
		steps.InstallDaemonBinaryStep(daemonSrc, paths),
	}

	// K8s-dependent components: cluster reachability + RBAC + kubeconfigs.
	if len(componentSpecs) > 0 {
		wfSteps = append(wfSteps,
			steps.CheckClusterStep(),
			steps.CreateDaemonRBACStep(componentSpecs),
			steps.WriteDaemonKubeconfigStep(componentSpecs),
		)
	}

	wfSteps = append(wfSteps,
		steps.InstallDaemonServiceStep(paths),
		steps.AddOperatorToWeaverGroupStep(),
	)

	return automa.NewWorkflowBuilder().WithId("daemon-service-install-workflow").Steps(wfSteps...), nil
}

// NewDaemonServiceUninstallWorkflow tears down the daemon stack in reverse:
//  1. Check root privileges
//  2. Stop + disable + remove systemd service unit
//  3. Remove per-component kubeconfig files
//  4. Delete per-component RBAC resources (CRB + CR + Secret + SA)
func NewDaemonServiceUninstallWorkflow() (*automa.WorkflowBuilder, error) {
	paths := models.Paths()
	componentSpecs, err := loadComponentSpecs(paths)
	if err != nil {
		return nil, err
	}
	return automa.NewWorkflowBuilder().WithId("daemon-service-uninstall-workflow").Steps(
		CheckPrivilegesStep(),
		steps.RemoveDaemonServiceStep(paths),
		steps.RemoveDaemonKubeconfigStep(componentSpecs),
		steps.DeleteDaemonRBACStep(componentSpecs),
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
