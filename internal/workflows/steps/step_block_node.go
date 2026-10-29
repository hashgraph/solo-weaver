// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/models"

	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"sigs.k8s.io/yaml"
)

const (
	SetupBlockNodeStepId             = "setup-block-node"
	SetupBlockNodeStorageStepId      = "setup-block-node-storage"
	CreateBlockNodeNamespaceStepId   = "create-block-node-namespace"
	CreateBlockNodePVsStepId         = "create-block-node-pvs"
	DeleteBlockNodePVsStepId         = "delete-block-node-pvs"
	RecreateBlockNodeStorageStepId   = "recreate-block-node-storage"
	UninstallBlockNodeStepId         = "uninstall-block-node"
	UpgradeBlockNodeStepId           = "upgrade-block-node"
	EnsureStorageMigrationsStepId    = "ensure-block-node-storage-migrations"
	WaitForBlockNodeStepId           = "wait-for-block-node"
	ResetBlockNodeStepId             = "reset-block-node"
	PurgeBlockNodeStorageStepId      = "purge-block-node-storage"
	ScaleDownBlockNodeStepId         = "scale-down-block-node"
	ClearBlockNodeStorageStepId      = "clear-block-node-storage"
	ScaleUpBlockNodeStepId           = "scale-up-block-node"
	WaitForBlockNodeTerminatedStepId = "wait-for-block-node-terminated"
	VerifyBlockNodeReachableStepId   = "verify-block-node-reachable"
)

// SetupBlockNode sets up the block node on the cluster.
//
// Weaver still owns the host-level prerequisites — the hedera owner, storage
// directories, namespace, and PVs — but the block-node workload is now deployed
// through solo-operator (#1319): weaver ensures the Orbit the HelmCapsule needs,
// applies a HelmCapsule (component: block-node) carrying the rendered chart
// values, and waits for the operator to reconcile it to Ready, instead of driving
// the Helm SDK directly. The install guard (PrecheckBlockNodeNotInstalled) lives
// in the install handler's preflight, mirroring the consensus-node install.
func SetupBlockNode(inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	blockNodeManagerProvider := newBlockNodeManagerProvider(inputs)
	valuesFn := func() ([]byte, error) {
		manager, err := blockNodeManagerProvider()
		if err != nil {
			return nil, err
		}
		return manager.ComputeValues(inputs.Profile, inputs.ValuesFile)
	}

	return automa.NewWorkflowBuilder().WithId(SetupBlockNodeStepId).Steps(
		EnsureHederaOwnerStep(),
		setupBlockNodeStorage(blockNodeManagerProvider),
		createBlockNodeNamespace(blockNodeManagerProvider),
		createBlockNodePVs(blockNodeManagerProvider),
		EnsureBlockNodeOrbit(inputs, DefaultCapsuleKubeProvider),
		// Install applies the fully-rendered values (no reuse — there is nothing to
		// reuse on a fresh node; a --force re-apply is intentionally a clean render).
		CreateBlockNodeCapsule(inputs, valuesFn, false, DefaultCapsuleKubeProvider),
		WaitForBlockNodeCapsuleReady(inputs, DefaultCapsuleKubeProvider),
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

// BlockNodeHelmInstalledFn returns a checker reporting whether a legacy,
// weaver-helm-managed block node release exists for these inputs. The install
// precheck (PrecheckBlockNodeNotInstalled) uses it to detect a pre-#1319 release
// that must be uninstalled before an operator-driven install. The manager and its
// cluster clients are built lazily, so the check runs only when the closure is
// invoked at step-execution time.
func BlockNodeHelmInstalledFn(inputs models.BlockNodeInputs) func() (bool, error) {
	managerProvider := newBlockNodeManagerProvider(inputs)
	return func() (bool, error) {
		manager, err := managerProvider()
		if err != nil {
			return false, err
		}
		return manager.IsInstalled()
	}
}

func UninstallBlockNode(inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	blockNodeManagerProvider := newBlockNodeManagerProvider(inputs)
	return automa.NewWorkflowBuilder().WithId(UninstallBlockNodeStepId).Steps(
		uninstallBlockNode(inputs, blockNodeManagerProvider, DefaultCapsuleKubeProvider),
	)
}

// uninstallBlockNode removes the block node workload, supporting both deployment
// models (#1319):
//
//   - Operator-managed (new installs): a HelmCapsule CR owns the release. Delete
//     the CR and let solo-operator uninstall the underlying Helm release via its
//     finalizer. Deleting the CR — not running helm uninstall ourselves — is what
//     keeps weaver from fighting the operator's reconcile.
//   - Legacy weaver-helm-managed (pre-#1319 installs on existing clusters): no CR
//     exists. Fall back to a direct Helm uninstall.
//
// Both paths are disk-safe — neither touches PVs/PVCs or on-disk data; storage
// teardown stays a separate, explicit step gated on --with-reset / --purge-storage.
// Both are idempotent: a missing CR falls through to the Helm path, and a missing
// Helm release is a no-op.
func uninstallBlockNode(
	inputs models.BlockNodeInputs,
	getManager func() (*blocknode.Manager, error),
	capsuleProvider CapsuleKubeProvider,
) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(UninstallBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := capsuleProvider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
			name := blockNodeCapsuleName(inputs)

			exists, err := kc.ResourceExists(ctx, apiVersion, string(kube.KindHelmCapsule), inputs.Namespace, name)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if exists {
				// Operator-managed: delete the CR; the operator uninstalls the release.
				if _, err := kc.DeleteResource(ctx, apiVersion, string(kube.KindHelmCapsule), inputs.Namespace, name); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
				logx.As().Info().Str("name", name).Msg("Deleted HelmCapsule; operator will uninstall the Helm release")
				return automa.StepSuccessReport(stp.Id())
			}

			// Legacy path: no operator CR, so uninstall any weaver-helm-managed release.
			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			installed, err := manager.IsInstalled()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if !installed {
				logx.As().Info().Str("release", inputs.Release).Msg("No HelmCapsule and no Helm release found; nothing to uninstall")
				return automa.StepSuccessReport(stp.Id())
			}

			if err := manager.UninstallChart(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			logx.As().Info().Str("release", inputs.Release).Msg("Uninstalled legacy Helm-managed block node release")
			return automa.StepSuccessReport(stp.Id())
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

// UpgradeBlockNode applies the block node's desired state through solo-operator
// (#1319): it patches the HelmCapsule with the requested chart version and
// freshly-rendered values (server-side apply) and waits for the operator to
// reconcile the release to Ready. One flow serves both `block node upgrade` (new
// chart version) and `block node reconfigure` (same version, changed values) — the
// operator upgrades the release whenever the chart spec or values hash changes.
//
// It replaces the former direct `helm upgrade`: there is no Cilium #619
// Service-delete workaround (the operator owns the Services) and no imperative
// post-upgrade scale-down — `--no-scale-up` is expressed declaratively as
// replicaCount: 0 in the rendered values (see Manager.ComputeValues).
//
// NOTE: the legacy breaking-chart-change storage migration (BuildMigrationWorkflow)
// is not yet re-homed onto this path; storage migrations that must run before a
// breaking chart upgrade are tracked as follow-up work.
func UpgradeBlockNode(inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	blockNodeManagerProvider := newBlockNodeManagerProvider(inputs)
	valuesFn := func() ([]byte, error) {
		manager, err := blockNodeManagerProvider()
		if err != nil {
			return nil, err
		}
		return manager.ComputeValues(inputs.Profile, inputs.ValuesFile)
	}

	// Service-shape guard (#619): record the deployed Service shape before the patch,
	// and after the operator reconciles, restart Cilium if the shape changed so the
	// dropped spec.type transition does not blackhole the LoadBalancer.
	recordShape, healCilium := BlockNodeServiceShapeGuard(inputs, DefaultCapsuleKubeProvider)

	return automa.NewWorkflowBuilder().WithId(UpgradeBlockNodeStepId).Steps(
		EnsureHederaOwnerStep(),
		// Breaking-boundary storage prep: create the PV/PVCs for any optional storage
		// a newer chart version requires, before the capsule patch renders the chart
		// against them. No-op when the upgrade crosses no such boundary.
		EnsureBlockNodeStorageMigrations(inputs),
		recordShape,
		// Upgrade/reconfigure honor --reuse-values (default true): preserve deployed
		// operator-set values under the merge; --no-reuse-values applies only the
		// fresh render.
		CreateBlockNodeCapsule(inputs, valuesFn, inputs.ReuseValues, DefaultCapsuleKubeProvider),
		WaitForBlockNodeCapsuleReady(inputs, DefaultCapsuleKubeProvider),
		healCilium,
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

// EnsureBlockNodeStorageMigrations creates the PV/PVCs for any optional storage a
// newer chart version requires before an operator upgrade patches the HelmCapsule
// (#1319). Weaver owns the PVs (existingClaim model), so a volume a breaking chart
// boundary introduces must exist before the operator renders the chart against it.
// It builds and runs the storage-prep workflow at execute time (the applicable set
// depends on the deployed vs target version) and is a no-op when the upgrade crosses
// no such boundary. It does NOT move the chart — that is the capsule patch that
// follows.
func EnsureBlockNodeStorageMigrations(inputs models.BlockNodeInputs) automa.Builder {
	managerProvider := newBlockNodeManagerProvider(inputs)
	return automa.NewStepBuilder().WithId(EnsureStorageMigrationsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			manager, err := managerProvider()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			wf, err := blocknode.BuildStorageMigrationWorkflow(manager, inputs.Profile, inputs.ValuesFile)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if wf == nil {
				// No breaking-boundary storage migration applies.
				return automa.StepSuccessReport(stp.Id())
			}

			workflow, err := wf.Build()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if report := workflow.Execute(ctx); report.Error != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(report.Error))
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Preparing storage for chart changes")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to prepare storage for chart changes")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Storage prepared for chart changes")
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
func purgeBlockNodeStorageSteps(inputs models.BlockNodeInputs, managerProvider func() (*blocknode.Manager, error), capsuleProvider CapsuleKubeProvider) []automa.Builder {
	return []automa.Builder{
		scaleDownBlockNode(inputs, managerProvider, capsuleProvider, ScaleDownBlockNodeStepId),
		waitForBlockNodeTerminated(managerProvider),
		clearBlockNodeStorage(managerProvider),
	}
}

// ResetBlockNode resets the block node by clearing all storage and, unless
// inputs.LeaveScaledDown is set, restarting the pod and waiting for it to be ready.
//
// deployedStorage is the storage configuration the block node is currently
// running with; the directories are cleared there rather than at
// inputs.Storage, which is where the operator wants them next. The two differ
// only under inputs.PurgeStorage, the one flag that can move a block node to new
// paths — a local PV's hostPath is immutable, so the PVs/PVCs are deleted and
// recreated between clearing the old directories and starting the pod again.
func ResetBlockNode(deployedStorage models.BlockNodeStorage, inputs models.BlockNodeInputs) *automa.WorkflowBuilder {
	deployedIns := inputs
	deployedIns.Storage = deployedStorage

	managerProvider := newBlockNodeManagerProvider(inputs)

	stepList := purgeBlockNodeStorageSteps(deployedIns, newBlockNodeManagerProvider(deployedIns), DefaultCapsuleKubeProvider)
	if inputs.PurgeStorage {
		stepList = append(stepList, RecreateBlockNodeStorage(inputs))
	}
	if !inputs.LeaveScaledDown {
		stepList = append(stepList,
			scaleUpBlockNode(inputs, managerProvider, DefaultCapsuleKubeProvider),
			waitForBlockNode(managerProvider),
		)
	}

	return automa.NewWorkflowBuilder().WithId(ResetBlockNodeStepId).Steps(stepList...).
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
		purgeBlockNodeStorageSteps(inputs, managerProvider, DefaultCapsuleKubeProvider)...,
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

// setBlockNodeReplicas scales the block node, choosing the mechanism by deployment
// model (#1319). When a HelmCapsule CR owns the block node it re-applies the
// capsule with the appropriate replicaCount (0 when scaledDown, chart default
// otherwise) so the change survives the operator's reconcile; a legacy,
// weaver-helm-managed release is scaled directly via the StatefulSet.
func setBlockNodeReplicas(ctx context.Context, inputs models.BlockNodeInputs, manager *blocknode.Manager, kc CapsuleKubeClient, scaledDown bool) error {
	apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
	name := blockNodeCapsuleName(inputs)

	exists, err := kc.ResourceExists(ctx, apiVersion, string(kube.KindHelmCapsule), inputs.Namespace, name)
	if err != nil {
		return err
	}

	if !exists {
		// Legacy release: no operator to fight, scale the StatefulSet directly.
		replicas := int32(1)
		if scaledDown {
			replicas = 0
		}
		return manager.ScaleStatefulSet(ctx, replicas)
	}

	// Operator-managed: scale declaratively by toggling replicaCount on the deployed
	// values and re-applying. This is a read-modify-write of the live spec.values —
	// it preserves every other deployed value (operator customizations included) and
	// does NOT re-render config, so a reset/scale never doubles as a reconfigure.
	deployed, _, err := kc.GetResourceNestedMap(ctx, apiVersion, string(kube.KindHelmCapsule), inputs.Namespace, name, "spec", "values")
	if err != nil {
		return err
	}
	if deployed == nil {
		deployed = map[string]interface{}{}
	}
	if scaledDown {
		deployed["replicaCount"] = 0
	} else {
		deployed["replicaCount"] = 1
	}
	valuesYAML, err := yaml.Marshal(deployed)
	if err != nil {
		return err
	}
	capsule, err := buildBlockNodeHelmCapsule(inputs, valuesYAML)
	if err != nil {
		return err
	}
	return kc.ApplyTyped(ctx, capsule)
}

// scaleDownBlockNode scales the block node to 0 replicas, dual-model: a HelmCapsule
// re-apply with replicaCount: 0 for operator-managed nodes, or a direct StatefulSet
// scale for a legacy release (see setBlockNodeReplicas). stepId is a parameter so a
// workflow that scales down in more than one place does not collide on the id.
func scaleDownBlockNode(inputs models.BlockNodeInputs, getManager func() (*blocknode.Manager, error), capsuleProvider CapsuleKubeProvider, stepId string) automa.Builder {
	return automa.NewStepBuilder().WithId(stepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := capsuleProvider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := setBlockNodeReplicas(ctx, inputs, manager, kc, true); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{ConfiguredByThisStep: "true"}
			stp.State().Local().Set(ConfiguredByThisStep, true)
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// On rollback, scale back up.
			if v, _ := stp.State().Local().Bool(ConfiguredByThisStep); v == false {
				return automa.StepSkippedReport(stp.Id())
			}
			kc, err := capsuleProvider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if err := setBlockNodeReplicas(ctx, inputs, manager, kc, false); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Scaling down Block Node")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to scale down Block Node")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node scaled down successfully")
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
func scaleUpBlockNode(inputs models.BlockNodeInputs, getManager func() (*blocknode.Manager, error), capsuleProvider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(ScaleUpBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := capsuleProvider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if err := setBlockNodeReplicas(ctx, inputs, manager, kc, false); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{ConfiguredByThisStep: "true"}
			stp.State().Local().Set(ConfiguredByThisStep, true)
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// On rollback, scale back down (though this is an unusual case).
			if v, _ := stp.State().Local().Bool(ConfiguredByThisStep); v == false {
				return automa.StepSkippedReport(stp.Id())
			}
			kc, err := capsuleProvider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			manager, err := getManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if err := setBlockNodeReplicas(ctx, inputs, manager, kc, true); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Scaling up Block Node")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to scale up Block Node")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block Node scaled up successfully")
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
