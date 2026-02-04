// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
)

const (
	SetupBlockNodeStepId           = "setup-block-node"
	SetupBlockNodeStorageStepId    = "setup-block-node-storage"
	CreateBlockNodeNamespaceStepId = "create-block-node-namespace"
	CreateBlockNodePVsStepId       = "create-block-node-pvs"
	InstallBlockNodeStepId         = "install-block-node"
	UpgradeBlockNodeStepId         = "upgrade-block-node"
	AnnotateBlockNodeServiceStepId = "annotate-block-node-service"
	WaitForBlockNodeStepId         = "wait-for-block-node"
)

// SetupBlockNode sets up the block node on the cluster
func SetupBlockNode(profile string, valuesFile string) *automa.WorkflowBuilder {
	// Lazy initialization of block node manager
	// This blocknodeManagerProvider pattern ensures that the manager is only created once
	// and reused across all steps in the workflow steps
	var blockNodeManager *blocknode.Manager
	blockNodeManagerProvider := func() (*blocknode.Manager, error) {
		if blockNodeManager == nil {
			var err error
			blockNodeManager, err = blocknode.NewManager(config.Get().BlockNode)
			if err != nil {
				return nil, err
			}
		}
		return blockNodeManager, nil
	}

	return automa.NewWorkflowBuilder().WithId(SetupBlockNodeStepId).Steps(
		setupBlockNodeStorage(blockNodeManagerProvider),
		createBlockNodeNamespace(blockNodeManagerProvider),
		createBlockNodePVs(blockNodeManagerProvider),
		installBlockNode(profile, valuesFile, blockNodeManagerProvider),
		annotateBlockNodeService(blockNodeManagerProvider),
		waitForBlockNode(blockNodeManagerProvider),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Block Node")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Block Node")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node setup successfully")
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
			stp.State().Set(ConfiguredByThisStep, true)

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

			err = manager.CreateNamespace(ctx, core.Paths().TempDir)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Set(ConfiguredByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if !stp.State().Bool(ConfiguredByThisStep) {
				return automa.StepSkippedReport(stp.Id())
			}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.DeleteNamespace(ctx, core.Paths().TempDir); err != nil {
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

// createBlockNodePVs creates the PersistentVolumes and PersistentVolumeClaims for block node
func createBlockNodePVs(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(CreateBlockNodePVsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.CreatePersistentVolumes(ctx, core.Paths().TempDir); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Set(ConfiguredByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if stp.State().Bool(ConfiguredByThisStep) == false {
				return automa.StepSkippedReport(stp.Id())
			}

			manager, err := blocknode.NewManager(config.Get().BlockNode)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.DeletePersistentVolumes(ctx, core.Paths().TempDir); err != nil {
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
				stp.State().Set(InstalledByThisStep, true)
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if stp.State().Bool(InstalledByThisStep) == false {
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

// annotateBlockNodeService annotates the block node service with MetalLB address pool
func annotateBlockNodeService(getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(AnnotateBlockNodeServiceStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := manager.AnnotateService(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Set(ConfiguredByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Annotating Block Node service")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to annotate Block Node service")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node service annotated successfully")
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

// UpgradeBlockNode upgrades the block node on the cluster
func UpgradeBlockNode(profile string, valuesFile string, reuseValues bool) *automa.WorkflowBuilder {
	// Lazy initialization of block node manager
	var blockNodeManager *blocknode.Manager
	blockNodeManagerProvider := func() (*blocknode.Manager, error) {
		if blockNodeManager == nil {
			var err error
			blockNodeManager, err = blocknode.NewManager(config.Get().BlockNode)
			if err != nil {
				return nil, err
			}
		}
		return blockNodeManager, nil
	}

	return automa.NewWorkflowBuilder().WithId(UpgradeBlockNodeStepId).Steps(
		upgradeBlockNode(profile, valuesFile, reuseValues, blockNodeManagerProvider),
		waitForBlockNode(blockNodeManagerProvider),
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

// upgradeBlockNode upgrades the block node helm chart
func upgradeBlockNode(profile string, valuesFile string, reuseValues bool, getManager func() (*blocknode.Manager, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(UpgradeBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Check if this upgrade requires migrations due to breaking chart changes
			migrationWorkflow, err := blocknode.GetMigrationWorkflow(manager, profile, valuesFile, reuseValues)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if migrationWorkflow != nil {
				logx.As().Info().Msg("Breaking chart change detected, performing automatic migration")

				workflow, err := migrationWorkflow.Build()
				if err != nil {
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

			// Normal upgrade path
			valuesFilePath, err := manager.ComputeValuesFile(profile, valuesFile)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = manager.UpgradeChart(ctx, valuesFilePath, reuseValues)
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
