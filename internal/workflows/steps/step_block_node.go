// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/pkg/models"

	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
)

const (
	SetupBlockNodeStepId             = "setup-block-node"
	SetupBlockNodeStorageStepId      = "setup-block-node-storage"
	CreateBlockNodeNamespaceStepId   = "create-block-node-namespace"
	CreateBlockNodePVsStepId         = "create-block-node-pvs"
	DeleteBlockNodePVsStepId         = "delete-block-node-pvs"
	RecreateBlockNodeStorageStepId   = "recreate-block-node-storage"
	InstallBlockNodeStepId           = "install-block-node"
	UninstallBlockNodeStepId         = "uninstall-block-node"
	UpgradeBlockNodeStepId           = "upgrade-block-node"
	WaitForBlockNodeStepId           = "wait-for-block-node"
	ResetBlockNodeStepId             = "reset-block-node"
	PurgeBlockNodeStorageStepId      = "purge-block-node-storage"
	ScaleDownBlockNodeStepId         = "scale-down-block-node"
	ClearBlockNodeStorageStepId      = "clear-block-node-storage"
	ScaleUpBlockNodeStepId           = "scale-up-block-node"
	WaitForBlockNodeTerminatedStepId = "wait-for-block-node-terminated"
	RolloutRestartBlockNodeStepId    = "rollout-restart-block-node"
	VerifyBlockNodeReachableStepId   = "verify-block-node-reachable"
)

// SetupBlockNode sets up the block node on the cluster
func SetupBlockNode(inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	blockNodeManagerProvider := newBlockNodeManagerProvider(inputs)

	return automa.NewWorkflowBuilder().WithId(SetupBlockNodeStepId).Steps(
		EnsureHederaOwnerStep(),
		setupBlockNodeStorage(blockNodeManagerProvider),
		createBlockNodeNamespace(blockNodeManagerProvider),
		createBlockNodePVs(blockNodeManagerProvider),
		installBlockNode(inputs.Profile, inputs.ValuesFile, blockNodeManagerProvider),
		waitForBlockNode(blockNodeManagerProvider),
		verifyBlockNodeReachable(blockNodeManagerProvider),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "Block Node Deployment")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "Block Node Deployment")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "Block Node Deployment")
		})
}

// setupBlockNodeStorage creates the required directories for block node storage
func setupBlockNodeStorage(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(SetupBlockNodeStorageStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.SetupStorage(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Local().Set(ConfiguredByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Block Node storage directories")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Block Node storage directories")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node storage directories setup successfully")
		})
}

// createBlockNodeNamespace creates the block-node namespace in the cluster
func createBlockNodeNamespace(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(CreateBlockNodeNamespaceStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = manager.CreateNamespace(ctx, models.Paths().TempDir)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Local().Set(ConfiguredByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if v, _ := stp.State().Local().Bool(ConfiguredByThisStep); v == false {
				return automa.StepSkippedReport(stp.Id())
			}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.DeleteNamespace(ctx, models.Paths().TempDir); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating Block Node namespace")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create Block Node namespace")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node namespace created successfully")
		})
}

// createBlockNodePVs creates the PersistentVolumes and PersistentVolumeClaims for blocknode
func createBlockNodePVs(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(CreateBlockNodePVsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.CreatePersistentVolumes(ctx, models.Paths().TempDir); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Local().Set(ConfiguredByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if v, _ := stp.State().Local().Bool(ConfiguredByThisStep); v == false {
				return automa.StepSkippedReport(stp.Id())
			}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.DeleteAllPersistentVolumes(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating Block Node PVs and PVCs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create Block Node PVs and PVCs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node PVs and PVCs created successfully")
		})
}

func UninstallBlockNode(inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	blockNodeManagerProvider := newBlockNodeManagerProvider(inputs)
	return automa.NewWorkflowBuilder().WithId(UninstallBlockNodeStepId).Steps(
		uninstallBlockNode(inputs.Profile, inputs.ValuesFile, blockNodeManagerProvider),
	)
}

// uninstallBlockNode uninstalls the block node helm chart
func uninstallBlockNode(profile string, valuesFile string, getManager func() (*blocknode.Manager, error)) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(InstallBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = manager.UninstallChart(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Uninstalling Block Node")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to uninstall Block Node")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node uninstalled successfully")
		})
}

// installBlockNode installs the block node helm chart
func installBlockNode(profile string, valuesFile string, getManager func() (*blocknode.Manager, error)) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(InstallBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			valuesFilePath, err := manager.ComputeValuesFile(profile, valuesFile)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			installed, err := manager.InstallChart(ctx, valuesFilePath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if !installed {
				meta[AlreadyInstalled] = "true"
			} else {
				meta[InstalledByThisStep] = "true"
				stp.State().Local().Set(InstalledByThisStep, true)
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if v, _ := stp.State().Local().Bool(InstalledByThisStep); v == false {
				return automa.StepSkippedReport(stp.Id())
			}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.UninstallChart(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing Block Node")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install Block Node")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node installed successfully")
		})
}

// waitForBlockNode waits for the block node pod to be ready
func waitForBlockNode(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(WaitForBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.WaitForPodReady(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Waiting for Block Node to be ready")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Block Node failed to become ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node is ready")
		})
}

// UpgradeBlockNode upgrades the block node on the cluster.
//
// The upgradeBlockNode step deletes the helm-owned Services immediately
// before calling helm so that helm recreates them as fresh CREATE events.
// Cilium's eBPF service reconciler drops the `spec.type` transition UPDATE
// event (the root cause of #619) but handles CREATE cleanly, so the
// topology flip heals itself without any kube-system-wide Cilium DaemonSet
// restart. The delete is placed AFTER preflight (migration discovery,
// values-file rendering) so a preflight failure leaves the Services intact
// — the failure window between delete and helm shrinks to a function call.
// The post-upgrade reachability probe converts any remaining failure mode
// (Cilium, MetalLB, chart, firewall) into a loud workflow error. See #644.
func UpgradeBlockNode(inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	blockNodeManagerProvider := newBlockNodeManagerProvider(inputs)

	return automa.NewWorkflowBuilder().WithId(UpgradeBlockNodeStepId).Steps(
		EnsureHederaOwnerStep(),
		upgradeBlockNode(inputs, blockNodeManagerProvider),
		waitForBlockNode(blockNodeManagerProvider),
		verifyBlockNodeReachable(blockNodeManagerProvider),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Upgrading Block Node")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to upgrade Block Node")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node upgraded successfully")
		})
}

// upgradeBlockNode upgrades the block node helm chart.
//
// Order matters: preflight (BuildMigrationWorkflow, ComputeValuesFile,
// migrationWorkflow.Build) runs before DeleteHelmOwnedServices so that any
// preflight failure leaves the Services intact and the cluster reachable.
// The Services are deleted only at the commit point — immediately before the
// helm operation that would have recreated them anyway — so the failure
// window between delete and helm is a function call. Helm's atomic mode
// covers anything that fails from UpgradeChart onward.
func upgradeBlockNode(inputs models.BlockNodeInputs, getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(UpgradeBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Preflight: any failure here is safe — Services intact, no outage.
			migrationWorkflow, err := blocknode.BuildMigrationWorkflow(manager, inputs.Profile, inputs.ValuesFile)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if migrationWorkflow != nil {
				logx.As().Info().Msg("Breaking chart change detected, performing automatic migration")

				workflow, err := migrationWorkflow.Build()
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}

				// Commit point: delete Services immediately before the helm
				// operation that the migration workflow will perform.
				if err := manager.DeleteHelmOwnedServices(ctx); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}

				report := workflow.Execute(ctx)
				if report.Error != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(report.Error))
				}

				meta["migrated"] = "true"
				meta["upgraded"] = "true"
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			// Normal upgrade path: preflight values computation first.
			valuesFilePath, err := manager.ComputeValuesFile(inputs.Profile, inputs.ValuesFile)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Commit point: delete Services immediately before helm upgrade.
			if err := manager.DeleteHelmOwnedServices(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = manager.UpgradeChart(ctx, valuesFilePath, inputs.ReuseValues)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta["upgraded"] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Upgrading Block Node chart")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to upgrade Block Node chart")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node chart upgraded successfully")
		})
}

// newBlockNodeManagerProvider creates a lazy-initialized block node manager provider
// This ensures that the manager is only created once and shared across steps, while also allowing for proper error handling during initialization.
func newBlockNodeManagerProvider(inputs models.BlockNodeInputs) func() (*blocknode.Manager, error) {
	var blockNodeManager *blocknode.Manager
	return func() (*blocknode.Manager, error) {
		if blockNodeManager == nil {
			var err error
			blockNodeManager, err = blocknode.NewManager(inputs)
			if err != nil {
				return nil, err
			}
		}
		return blockNodeManager, nil
	}
}

// purgeBlockNodeStorageSteps returns the steps to scale down, wait for termination, and clear storage
func purgeBlockNodeStorageSteps(managerProvider func() (*blocknode.Manager, error)) []automa.Builder {
	return []automa.Builder{
		scaleDownBlockNode(managerProvider),
		waitForBlockNodeTerminated(managerProvider),
		clearBlockNodeStorage(managerProvider),
	}
}

// ResetBlockNode resets the block node by clearing all storage and restarting the pod
func ResetBlockNode(inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	managerProvider := newBlockNodeManagerProvider(inputs)

	return automa.NewWorkflowBuilder().WithId(ResetBlockNodeStepId).Steps(
		append(purgeBlockNodeStorageSteps(managerProvider),
			scaleUpBlockNode(managerProvider),
			waitForBlockNode(managerProvider),
		)...,
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Resetting Block Node")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to reset Block Node")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node reset successfully")
		})
}

// DeleteBlockNodePersistentVolumes returns the step that deletes the block
// node's PVCs and PVs by label selector. Used by the uninstall --purge-storage
// workflow after the data directories have been wiped.
//
// This is a thin public facade over the package-private deleteBlockNodePVs
// helper; handlers in internal/bll/blocknode/ cannot reach the helper directly.
// The inner step already carries its own DeleteBlockNodePVsStepId and notify
// hooks (StepStart/StepFailure/StepCompletion), so no wrapper workflow is
// added here — wrapping would either collide on the step id or duplicate the
// notifications.
func DeleteBlockNodePersistentVolumes(inputs models.BlockNodeInputs) automa.Builder {
	return deleteBlockNodePVs(newBlockNodeManagerProvider(inputs))
}

// PurgeBlockNodeStorage scales down the block node and clears all storage.
// This does NOT scale back up - use ResetBlockNode if you need to restart the pod after clearing.
func PurgeBlockNodeStorage(inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	managerProvider := newBlockNodeManagerProvider(inputs)

	return automa.NewWorkflowBuilder().WithId(PurgeBlockNodeStorageStepId).Steps(
		purgeBlockNodeStorageSteps(managerProvider)...,
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Purging Block Node storage")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to purge Block Node storage")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node storage purged")
		})
}

// scaleDownBlockNode scales down the block node StatefulSet to 0 replicas
func scaleDownBlockNode(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(ScaleDownBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.ScaleStatefulSet(ctx, 0); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Local().Set(ConfiguredByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// On rollback, scale back up
			if v, _ := stp.State().Local().Bool(ConfiguredByThisStep); v == false {
				return automa.StepSkippedReport(stp.Id())
			}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.ScaleStatefulSet(ctx, 1); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Scaling down Block Node StatefulSet")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to scale down Block Node StatefulSet")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node StatefulSet scaled down successfully")
		})
}

// waitForBlockNodeTerminated waits for all block node pods to terminate
func waitForBlockNodeTerminated(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(WaitForBlockNodeTerminatedStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.WaitForPodsTerminated(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Waiting for Block Node pods to terminate")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Block Node pods failed to terminate")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node pods terminated")
		})
}

// clearBlockNodeStorage clears all block node storage directories
func clearBlockNodeStorage(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(ClearBlockNodeStorageStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.ResetStorage(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Clearing Block Node storage")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to clear Block Node storage")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node storage cleared successfully")
		})
}

// scaleUpBlockNode scales up the block node StatefulSet to 1 replica
func scaleUpBlockNode(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(ScaleUpBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.ScaleStatefulSet(ctx, 1); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Local().Set(ConfiguredByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// On rollback, scale back down (though this is an unusual case)
			if v, _ := stp.State().Local().Bool(ConfiguredByThisStep); v == false {
				return automa.StepSkippedReport(stp.Id())
			}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.ScaleStatefulSet(ctx, 0); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Scaling up Block Node StatefulSet")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to scale up Block Node StatefulSet")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node StatefulSet scaled up successfully")
		})
}

// restartBlockNodeSteps returns the four-step sequence that scales the block node
// StatefulSet down, waits for termination, scales it back up, and waits for readiness.
// It is the building block for RolloutRestartBlockNode and conceptually mirrors
// purgeBlockNodeStorageSteps for the data-clearing path.
func restartBlockNodeSteps(managerProvider func() (*blocknode.Manager, error)) []automa.Builder {
	return []automa.Builder{
		scaleDownBlockNode(managerProvider),
		waitForBlockNodeTerminated(managerProvider),
		scaleUpBlockNode(managerProvider),
		waitForBlockNode(managerProvider),
	}
}

// RolloutRestartBlockNode restarts the block node pod by scaling the StatefulSet
// down to 0, waiting for termination, scaling back up to 1, and waiting for readiness.
// This reuses the existing ScaleStatefulSet infrastructure and guarantees the pod
// picks up any configuration changes (including ConfigMap-only updates) that Helm
// did not propagate via a pod-spec diff.
func RolloutRestartBlockNode(inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	managerProvider := newBlockNodeManagerProvider(inputs)

	return automa.NewWorkflowBuilder().WithId(RolloutRestartBlockNodeStepId).Steps(
		restartBlockNodeSteps(managerProvider)...,
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Restarting Block Node pod")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to restart Block Node pod")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node pod restarted successfully")
		})
}

// deleteBlockNodePVs deletes all known block-node PVs and PVCs by name.
func deleteBlockNodePVs(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(DeleteBlockNodePVsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if err := manager.DeleteAllPersistentVolumes(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Deleting Block Node PVs and PVCs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to delete Block Node PVs and PVCs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node PVs and PVCs deleted")
		})
}

// RecreateBlockNodeStorage deletes existing PVs/PVCs, creates storage directories at the
// new paths, then creates fresh PVs/PVCs bound to those directories.
// It is used in the reconfigure --with-reset workflow to apply storage path changes.
func RecreateBlockNodeStorage(inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	managerProvider := newBlockNodeManagerProvider(inputs)

	return automa.NewWorkflowBuilder().WithId(RecreateBlockNodeStorageStepId).Steps(
		deleteBlockNodePVs(managerProvider),
		setupBlockNodeStorage(managerProvider),
		createBlockNodePVs(managerProvider),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Recreating Block Node storage")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to recreate Block Node storage")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node storage recreated successfully")
		})
}

// verifyBlockNodeReachable TCP-dials the block-node LoadBalancer Service from
// the solo-provisioner host process. No-ops when LoadBalancerEnabled is false
// (local profile). See `blocknode.Manager.VerifyExternalReachable`.
func verifyBlockNodeReachable(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(VerifyBlockNodeReachableStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if err := manager.VerifyExternalReachable(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Verifying Block Node external reachability")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Block Node is not reachable from the provisioner host")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node external reachability verified")
		})
}
