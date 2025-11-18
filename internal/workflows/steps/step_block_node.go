package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-weaver/internal/blocknode"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/workflows/notify"
)

const (
	SetupBlockNodeStepId           = "setup-block-node"
	SetupBlockNodeStorageStepId    = "setup-block-node-storage"
	CreateBlockNodeNamespaceStepId = "create-block-node-namespace"
	CreateBlockNodePVsStepId       = "create-block-node-pvs"
	InstallBlockNodeStepId         = "install-block-node"
	AnnotateBlockNodeServiceStepId = "annotate-block-node-service"
	WaitForBlockNodeStepId         = "wait-for-block-node"
)

// SetupBlockNode sets up the block node on the cluster
func SetupBlockNode(nodeType string) automa.Builder {
	return automa.NewWorkflowBuilder().WithId(SetupBlockNodeStepId).Steps(
		setupBlockNodeStorage(),
		createBlockNodeNamespace(),
		createBlockNodePVs(),
		installBlockNode(nodeType),
		annotateBlockNodeService(),
		waitForBlockNode(),
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
func setupBlockNodeStorage() automa.Builder {
	return automa.NewStepBuilder().WithId(SetupBlockNodeStorageStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := blocknode.NewManager()
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
func createBlockNodeNamespace() automa.Builder {
	return automa.NewStepBuilder().WithId(CreateBlockNodeNamespaceStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := blocknode.NewManager()
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

			manager, err := blocknode.NewManager()
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
func createBlockNodePVs() automa.Builder {
	return automa.NewStepBuilder().WithId(CreateBlockNodePVsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := blocknode.NewManager()
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

			manager, err := blocknode.NewManager()
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
func installBlockNode(nodeType string) automa.Builder {
	return automa.NewStepBuilder().WithId(InstallBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := blocknode.NewManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			installed, err := manager.InstallChart(ctx, core.Paths().TempDir, nodeType)
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

			manager, err := blocknode.NewManager()
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
func annotateBlockNodeService() automa.Builder {
	return automa.NewStepBuilder().WithId(AnnotateBlockNodeServiceStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := blocknode.NewManager()
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
func waitForBlockNode() automa.Builder {
	return automa.NewStepBuilder().WithId(WaitForBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			manager, err := blocknode.NewManager()
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
