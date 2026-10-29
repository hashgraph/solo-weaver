// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// Block-node operator-driven lifecycle steps. These mirror the consensus-node
// capsule steps (step_consensus_capsule.go) but target a generic HelmCapsule with
// component=block-node: the operator runs the upstream block-node-server chart
// server-side instead of weaver driving the Helm SDK directly (#1319). They share
// this package's CapsuleKubeClient / CapsuleKubeProvider with the consensus steps.
const (
	PrecheckBlockNodeStepId            = "precheck-block-node"
	PrecheckBlockNodeLegacyStepId      = "precheck-block-node-legacy"
	EnsureBlockNodeOrbitStepId         = "ensure-block-node-orbit"
	CreateBlockNodeCapsuleStepId       = "create-block-node-capsule"
	WaitForBlockNodeCapsuleReadyStepId = "wait-block-node-capsule-ready"
	DeleteBlockNodeCapsuleStepId       = "delete-block-node-capsule"
	RecordBlockNodeServiceShapeStepId  = "record-block-node-service-shape"
	HealCiliumServiceShapeStepId       = "heal-cilium-on-service-shape-change"
)

// Cilium DaemonSet coordinates for the #619 service-shape heal.
const (
	ciliumNamespace      = "kube-system"
	ciliumDaemonSet      = "cilium"
	ciliumRolloutTimeout = 5 * time.Minute
)

// blockNodeCapsuleReadyPollInterval is how often WaitForBlockNodeCapsuleReady
// re-reads the HelmCapsule status while waiting for it to become Ready.
const blockNodeCapsuleReadyPollInterval = 5 * time.Second

// blockNodeCapsuleName is the HelmCapsule (and Helm release) name for a block
// node. It is the operator-supplied release name, falling back to "block-node"
// — the name used throughout the solo-operator examples.
func blockNodeCapsuleName(inputs models.BlockNodeInputs) string {
	return valueOrDefault(inputs.Release, "block-node")
}

// blockNodeOrbitName is the cluster-scoped Orbit the block node's HelmCapsule
// references. HelmCapsule.orbitRef is required by the CRD even though the
// block-node component injects nothing from the Orbit topology. Weaver ensures
// its own Orbit named after the block node's namespace, matching the consensus
// convention of one Orbit per namespace.
func blockNodeOrbitName(inputs models.BlockNodeInputs) string {
	return inputs.Namespace
}

// splitBlockNodeChart resolves the HelmChartSpec repo + chart name from the
// block node inputs. The operator needs the repository and the chart name as
// separate fields, whereas weaver's Chart field may carry the whole reference
// (e.g. "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"). When
// ChartName is set it is authoritative and Chart is the bare repo; otherwise the
// last path segment of Chart is taken as the chart name.
func splitBlockNodeChart(inputs models.BlockNodeInputs) (repo, chart string) {
	ref := strings.TrimSpace(inputs.Chart)
	if name := strings.TrimSpace(inputs.ChartName); name != "" {
		return strings.TrimSuffix(ref, "/"+name), name
	}
	if i := strings.LastIndex(ref, "/"); i != -1 {
		return ref[:i], ref[i+1:]
	}
	return ref, ref
}

// resolveCapsuleTimeout picks the operator-supplied --timeout for the Helm
// install/upgrade the operator runs, falling back to the Helm CLI default (5m)
// when unset — the same budget the Helm-SDK path used.
func resolveCapsuleTimeout(inputs models.BlockNodeInputs) time.Duration {
	if inputs.Timeout > 0 {
		return inputs.Timeout
	}
	return 5 * time.Minute
}

// buildBlockNodeHelmCapsule assembles the HelmCapsule CR from the block node
// inputs and the fully-rendered Helm values (YAML). The values are the single
// source of truth already produced by blocknode.Manager.ComputeValues, converted
// here to JSON for spec.values (which the operator merges last, over any
// auto-injected or ValuesFrom values).
func buildBlockNodeHelmCapsule(inputs models.BlockNodeInputs, valuesYAML []byte) (*operatorv1alpha1.HelmCapsule, error) {
	repo, chart := splitBlockNodeChart(inputs)
	component := operatorv1alpha1.HelmComponentBlockNode

	spec := operatorv1alpha1.HelmCapsuleSpec{
		Chart: operatorv1alpha1.HelmChartSpec{
			Repo:    repo,
			Chart:   chart,
			Version: inputs.ChartVersion,
		},
		ReleaseName: blockNodeCapsuleName(inputs),
		Component:   &component,
		OrbitRef:    &corev1.LocalObjectReference{Name: blockNodeOrbitName(inputs)},
		// Mirror the one-shot Helm-SDK options weaver used: atomic rollback on a
		// failed install/upgrade, waiting for resources to become ready, within the
		// --timeout budget. The operator still reconciles continuously afterwards.
		Atomic:  true,
		Wait:    true,
		Timeout: &metav1.Duration{Duration: resolveCapsuleTimeout(inputs)},
	}

	if len(valuesYAML) > 0 {
		jsonBytes, err := yaml.YAMLToJSON(valuesYAML)
		if err != nil {
			return nil, errorx.IllegalFormat.Wrap(err, "failed to convert block node values to JSON for the HelmCapsule")
		}
		spec.Values = &apiextv1.JSON{Raw: jsonBytes}
	}

	return &operatorv1alpha1.HelmCapsule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion,
			Kind:       string(kube.KindHelmCapsule),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      blockNodeCapsuleName(inputs),
			Namespace: inputs.Namespace,
		},
		Spec: spec,
	}, nil
}

// PrecheckBlockNode is the new, CR-model install guard (#1319): it fails when the
// block node's HelmCapsule already exists, unless --force is set (re-apply path).
// It only inspects the operator CR — the legacy Helm release is a separate concern,
// guarded by PrecheckBlockNodeLegacy, so install composes the two. Mirrors
// PrecheckConsensusNotInstalled.
func PrecheckBlockNode(inputs models.BlockNodeInputs, force bool, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(PrecheckBlockNodeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
			name := blockNodeCapsuleName(inputs)

			exists, err := kc.ResourceExists(ctx, apiVersion, string(kube.KindHelmCapsule), inputs.Namespace, name)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to check HelmCapsule %s", name),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and RBAC for reading HelmCapsule resources")))
			}

			if exists && !force {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.New("block node is already installed (HelmCapsule %q exists); cannot install again — pass --force to re-apply", name),
					reasons.PreconditionNotMet,
					"Pass --force to re-apply the install over the existing block node",
					"Or use 'solo-provisioner block node uninstall' first",
					"Or use 'solo-provisioner block node upgrade' to move to a new chart version")))
			}
			if exists {
				logx.As().Warn().Msgf("HelmCapsule %q already exists — re-applying because --force was passed", name)
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking block node not already installed")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Block node already installed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block node install guard passed")
		})
}

// PrecheckBlockNodeLegacy is the legacy-model guard (#1319): it fails when a
// legacy, weaver-helm-managed block node release exists with no HelmCapsule CR.
// Every operator-driven command composes it so a legacy release can never be acted
// on by the operator path:
//
//   - HelmCapsule CR present (operator-managed) → pass; not a legacy release.
//   - No CR but a legacy Helm release present → refuse and direct the operator to
//     uninstall then reinstall to migrate. --force does not bypass this.
//   - Nothing installed → pass; the caller's own not-installed gate reports that.
//
// legacyInstalled reports whether a legacy Helm release exists; it is called only
// when no CR is found, and may be nil in unit tests that exercise only the CR path.
func PrecheckBlockNodeLegacy(inputs models.BlockNodeInputs, provider CapsuleKubeProvider, legacyInstalled func() (bool, error)) automa.Builder {
	return automa.NewStepBuilder().WithId(PrecheckBlockNodeLegacyStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
			name := blockNodeCapsuleName(inputs)

			exists, err := kc.ResourceExists(ctx, apiVersion, string(kube.KindHelmCapsule), inputs.Namespace, name)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to check HelmCapsule %s", name),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and RBAC for reading HelmCapsule resources")))
			}
			if exists {
				return automa.StepSuccessReport(stp.Id())
			}

			if legacyInstalled != nil {
				installed, err := legacyInstalled()
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(err, "failed to check for an existing Helm-managed block node release"),
						reasons.PreconditionNotMet,
						"Verify cluster connectivity and that 'helm list -n "+inputs.Namespace+"' works")))
				}
				if installed {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.New("a legacy Helm-managed block node is installed in namespace %q; this command operates through solo-operator and cannot act on a legacy release", inputs.Namespace),
						reasons.PreconditionNotMet,
						"Run 'solo-provisioner block node uninstall' to remove the legacy release (your storage is preserved), then run install again to move it under solo-operator",
						"Uninstall is disk-safe: it removes only the Helm release, not the PVs/PVCs or on-disk data")))
				}
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking block node is operator-managed")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Block node is not operator-managed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block node operator-managed check passed")
		})
}

// EnsureBlockNodeOrbit creates the cluster-scoped Orbit the block node's
// HelmCapsule references, if it does not already exist. HelmCapsule.orbitRef is
// required by the CRD; the block-node component injects nothing from the Orbit,
// so a minimal Orbit (name only) is sufficient. An existing Orbit (e.g. one a
// consensus node created) is reused unchanged.
func EnsureBlockNodeOrbit(inputs models.BlockNodeInputs, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(EnsureBlockNodeOrbitStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			orbitName := blockNodeOrbitName(inputs)
			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion

			exists, err := kc.ResourceExists(ctx, apiVersion, string(kube.KindOrbit), "", orbitName)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to check Orbit %s", orbitName),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and that your kubeconfig has RBAC to read the cluster-scoped solo-operator Orbit CR")))
			}

			if exists {
				logx.As().Info().Str("orbit", orbitName).Msg("Orbit already exists, skipping creation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
					AlreadyInstalled: "true",
				}))
			}

			orbit := &operatorv1alpha1.Orbit{
				TypeMeta: metav1.TypeMeta{
					APIVersion: apiVersion,
					Kind:       string(kube.KindOrbit),
				},
				ObjectMeta: metav1.ObjectMeta{Name: orbitName},
			}

			if err := kc.ApplyTyped(ctx, orbit); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to apply Orbit %s", orbitName),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and that your kubeconfig has RBAC to create/update the cluster-scoped solo-operator Orbit CR",
					"Check the solo-operator is installed and running")))
			}

			logx.As().Info().Str("orbit", orbitName).Msg("Orbit created successfully")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				InstalledByThisStep: "true",
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Ensuring Orbit CR for block node")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to ensure Orbit CR")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Orbit CR ensured")
		})
}

// CreateBlockNodeCapsule applies the block node's HelmCapsule CR via server-side
// apply. valuesFn renders the full chart values (blocknode.Manager.ComputeValues)
// at execute time — deferred because the values come from a lazily-built manager
// that needs a cluster client. This replaces the direct helm install/upgrade the
// Helm-SDK path performed; the operator reconciles the release from the CR.
//
// reuseValues implements --reuse-values under the operator model: when true, the
// freshly-rendered values are deep-merged on top of the deployed spec.values
// (weaver-managed keys win, operator extras persist); when false, only the fresh
// render is applied. Install passes false; upgrade/reconfigure pass the flag.
func CreateBlockNodeCapsule(inputs models.BlockNodeInputs, valuesFn func() ([]byte, error), reuseValues bool, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(CreateBlockNodeCapsuleStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			valuesYAML, err := valuesFn()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to render block node Helm values"),
					reasons.InvalidArgument,
					"Check the --values file and the resolved storage/retention/plugin settings")))
			}

			// --reuse-values: deep-merge the fresh render on top of the deployed
			// spec.values so operator-set values that weaver does not render are
			// preserved across upgrade/reconfigure (the operator-model equivalent of
			// helm --reuse-values). --no-reuse-values skips this and applies only the
			// fresh render.
			if reuseValues {
				deployedMap, found, derr := kc.GetResourceNestedMap(ctx,
					kube.SoloOperatorGroup+"/"+kube.SoloOperatorVersion, string(kube.KindHelmCapsule),
					inputs.Namespace, blockNodeCapsuleName(inputs), "spec", "values")
				if derr != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(derr, "failed to read deployed HelmCapsule values for --reuse-values"),
						reasons.PreconditionNotMet,
						"Re-run with --no-reuse-values to apply only the freshly-rendered values")))
				}
				if found {
					deployedYAML, merr := yaml.Marshal(deployedMap)
					if merr != nil {
						return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
							errorx.InternalError.Wrap(merr, "failed to marshal deployed values for --reuse-values"),
							reasons.Internal, "Re-run with --no-reuse-values")))
					}
					valuesYAML, err = blocknode.MergeDeployedValues(deployedYAML, valuesYAML)
					if err != nil {
						return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
							errorx.IllegalState.Wrap(err, "failed to merge deployed values for --reuse-values"),
							reasons.InvalidArgument, "Re-run with --no-reuse-values")))
					}
				}
			}

			// --no-restart (reconfigure only): preserve the deployed config-hash so the
			// freshly-computed values don't change the pod template and the operator does
			// not roll the pod. ComputeValues always injects a fresh hash; here we read the
			// hash on the live CR and substitute it (empty when none is deployed yet, which
			// removes the annotation to match a never-hashed pod). Install and upgrade never
			// set NoRestart, so this is inert for them.
			if inputs.NoRestart {
				apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
				name := blockNodeCapsuleName(inputs)
				deployedHash, _ := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindHelmCapsule), inputs.Namespace, name, "spec", "values", "podAnnotations", blocknode.ConfigHashAnnotation)
				valuesYAML, err = blocknode.SetConfigHash(valuesYAML, deployedHash)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(err, "failed to preserve deployed config hash for --no-restart"),
						reasons.Internal,
						"Retry without --no-restart to apply the configuration and roll the pod")))
				}
			}

			capsule, err := buildBlockNodeHelmCapsule(inputs, valuesYAML)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					err, reasons.InvalidArgument,
					"Check the rendered block node values and --chart-repo/--chart-version")))
			}

			if err := kc.ApplyTyped(ctx, capsule); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to apply HelmCapsule %s", capsule.Name),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and that your kubeconfig has RBAC to create/update solo-operator HelmCapsule CRs",
					"If the operator rejected the capsule (admission), read its message — a missing orbitRef or an invalid chart ref/version is the usual cause")))
			}

			logx.As().Info().Str("name", capsule.Name).Str("namespace", capsule.Namespace).Msg("HelmCapsule applied successfully")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				InstalledByThisStep: "true",
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Applying block node HelmCapsule")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to apply block node HelmCapsule")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block node HelmCapsule applied")
		})
}

// WaitForBlockNodeCapsuleReady blocks until the block node's HelmCapsule reports
// a terminal phase, or the --timeout budget elapses. The Helm-SDK path blocked
// synchronously inside helm (Wait=true); the operator reconciles asynchronously,
// so weaver must poll the CR status before any downstream host/kube step runs
// against the workload. Running/Succeeded succeed; Failed fails with the operator
// message; a timeout fails with a raise-the-timeout hint.
//
// Readiness is currently gauged by status.phase. A condition-based check
// (Ready=True) would be stricter — tracked as a Phase-2 refinement once the exact
// condition the operator sets for block-node is confirmed.
func WaitForBlockNodeCapsuleReady(inputs models.BlockNodeInputs, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(WaitForBlockNodeCapsuleReadyStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
			name := blockNodeCapsuleName(inputs)
			deadline := time.Now().Add(resolveCapsuleTimeout(inputs))

			for {
				phase, _ := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindHelmCapsule), inputs.Namespace, name, "status", "phase")
				switch operatorv1alpha1.Phase(phase) {
				case operatorv1alpha1.PhaseRunning, operatorv1alpha1.PhaseSucceeded:
					notify.As().StepDetail(ctx, stp, fmt.Sprintf("block node ready (phase=%s)", phase))
					return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{"phase": phase}))
				case operatorv1alpha1.PhaseFailed:
					releaseStatus, _ := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindHelmCapsule), inputs.Namespace, name, "status", "releaseStatus")
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.New("block node HelmCapsule %q reported phase=Failed (releaseStatus=%q)", name, releaseStatus),
						reasons.PreconditionNotMet,
						fmt.Sprintf("Inspect the capsule: kubectl -n %s describe helmcapsule %s", inputs.Namespace, name),
						"Check the block node pod and events for the underlying failure")))
				}

				if time.Now().After(deadline) {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.TimeoutElapsed.New("timed out waiting for block node HelmCapsule %q to become ready (last phase=%q)", name, phase),
						reasons.PreconditionNotMet,
						"Raise --timeout if the block node legitimately needs longer to start",
						fmt.Sprintf("Inspect the capsule: kubectl -n %s describe helmcapsule %s", inputs.Namespace, name))))
				}

				select {
				case <-ctx.Done():
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(ctx.Err(), "waiting for block node HelmCapsule %q was cancelled", name),
						reasons.PreconditionNotMet,
						"Re-run the command")))
				case <-time.After(blockNodeCapsuleReadyPollInterval):
				}
			}
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Waiting for block node to become ready")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Block node did not become ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block node is ready")
		})
}

// DeleteBlockNodeCapsule removes the block node's HelmCapsule CR. The operator's
// finalizer then runs `helm uninstall`, tearing down the release's workload
// (StatefulSet, pods, Services). This is deliberately disk-safe: it deletes only
// the CR, never the PVs/PVCs or their on-disk data — storage teardown remains a
// separate, explicit weaver step (PurgeBlockNodeStorage /
// DeleteBlockNodePersistentVolumes) gated on --with-reset / --purge-storage. A
// missing CR is treated as success so uninstall is idempotent.
func DeleteBlockNodeCapsule(inputs models.BlockNodeInputs, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(DeleteBlockNodeCapsuleStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
			name := blockNodeCapsuleName(inputs)

			existed, err := kc.DeleteResource(ctx, apiVersion, string(kube.KindHelmCapsule), inputs.Namespace, name)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to delete HelmCapsule %s", name),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and that your kubeconfig has RBAC to delete solo-operator HelmCapsule CRs")))
			}

			if !existed {
				logx.As().Info().Str("name", name).Msg("HelmCapsule not found, nothing to delete")
			} else {
				logx.As().Info().Str("name", name).Msg("HelmCapsule deleted; operator will uninstall the Helm release")
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing block node HelmCapsule")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove block node HelmCapsule")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block node HelmCapsule removed")
		})
}

// blockNodeServiceShape carries the Service "shape signature" recorded before an
// operator upgrade/reconfigure so the heal step can tell whether it changed.
type blockNodeServiceShape struct {
	before   string
	recorded bool
}

// BlockNodeServiceShapeGuard returns a pair of steps that detect a block-node
// Service shape change across an operator upgrade/reconfigure and, when one
// happens, restart the Cilium DaemonSet. Cilium's eBPF reconciler drops a Service
// spec.type transition UPDATE (ClusterIP<->LoadBalancer) and silently blackholes
// the LoadBalancer (issue #619); a restart rebuilds the entry. The guard only
// matters for a deliberate shape change (e.g. reconfigure --values toggling
// loadBalancer.enabled) — normal install/upgrade/reconfigure keep a consistent
// shape and never trigger it.
//
// record runs before the capsule patch; heal runs after the operator has
// reconciled the new Service (post wait-ready). Detection is best-effort — a read
// failure never fails the operation — but a Cilium restart failure does fail, so
// the operator is not left with a silently broken LoadBalancer.
func BlockNodeServiceShapeGuard(inputs models.BlockNodeInputs, provider CapsuleKubeProvider) (record, heal automa.Builder) {
	shape := &blockNodeServiceShape{}
	return recordBlockNodeServiceShape(inputs, provider, shape), healCiliumOnServiceShapeChange(inputs, provider, shape)
}

func recordBlockNodeServiceShape(inputs models.BlockNodeInputs, provider CapsuleKubeProvider, shape *blockNodeServiceShape) automa.Builder {
	return automa.NewStepBuilder().WithId(RecordBlockNodeServiceShapeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				logx.As().Debug().Err(err).Msg("Service-shape guard: cannot reach cluster to record shape; skipping")
				return automa.StepSuccessReport(stp.Id())
			}
			sig, err := blockNodeServiceSignature(ctx, kc, inputs)
			if err != nil {
				logx.As().Debug().Err(err).Msg("Service-shape guard: could not record deployed Service shape; skipping")
				return automa.StepSuccessReport(stp.Id())
			}
			shape.before = sig
			shape.recorded = true
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Recording block node Service shape")
			return ctx, nil
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block node Service shape recorded")
		})
}

func healCiliumOnServiceShapeChange(inputs models.BlockNodeInputs, provider CapsuleKubeProvider, shape *blockNodeServiceShape) automa.Builder {
	return automa.NewStepBuilder().WithId(HealCiliumServiceShapeStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if !shape.recorded {
				// Couldn't record the before-shape; the reachability probe is the safety net.
				return automa.StepSuccessReport(stp.Id())
			}
			kc, err := provider(ctx)
			if err != nil {
				logx.As().Debug().Err(err).Msg("Service-shape guard: cannot reach cluster to compare shape; skipping")
				return automa.StepSuccessReport(stp.Id())
			}
			after, err := blockNodeServiceSignature(ctx, kc, inputs)
			if err != nil {
				logx.As().Debug().Err(err).Msg("Service-shape guard: could not read current Service shape; skipping")
				return automa.StepSuccessReport(stp.Id())
			}
			if after == shape.before {
				return automa.StepSuccessReport(stp.Id())
			}

			logx.As().Info().
				Str("before", shape.before).
				Str("after", after).
				Msg("Block node Service shape changed; restarting Cilium to rebuild eBPF LoadBalancer entries (#619)")

			if err := restartCiliumDaemonSet(ctx); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to restart Cilium after a block node Service shape change"),
					reasons.PreconditionNotMet,
					"Restart Cilium manually so it rebuilds the LoadBalancer entry: kubectl rollout restart ds/cilium -n kube-system",
					"Then verify external reachability to the block node LoadBalancer IP")))
			}
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{"ciliumRestarted": "true"}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking block node Service shape for Cilium reconcile")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to heal Cilium after a Service shape change")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Block node Service shape checked")
		})
}

// blockNodeServiceSignature returns a stable signature of the block node's Services
// — a sorted "name:spec.type" list — so a shape change (a spec.type transition, or
// the -external Service appearing/disappearing) shows up as a different string.
func blockNodeServiceSignature(ctx context.Context, kc CapsuleKubeClient, inputs models.BlockNodeInputs) (string, error) {
	opts := kube.WaitOptions{LabelSelector: "app.kubernetes.io/instance=" + blockNodeCapsuleName(inputs)}
	list, err := kc.List(ctx, kube.KindService, inputs.Namespace, opts)
	if err != nil {
		return "", err
	}
	entries := make([]string, 0, len(list.Items))
	for i := range list.Items {
		t, _, _ := unstructured.NestedString(list.Items[i].Object, "spec", "type")
		entries = append(entries, list.Items[i].GetName()+":"+t)
	}
	sort.Strings(entries)
	return strings.Join(entries, ";"), nil
}

// restartCiliumDaemonSet rolls the Cilium DaemonSet and waits for the rollout to
// finish, so the agents rebuild the eBPF LoadBalancer entries Cilium dropped on a
// Service spec.type transition. The agent restart is safe — eBPF programs stay
// loaded, so established connections survive; only the control plane bounces.
func restartCiliumDaemonSet(ctx context.Context) error {
	kc, err := kube.NewClient()
	if err != nil {
		return err
	}
	if err := kc.RolloutRestart(ctx, kube.KindDaemonSet, ciliumNamespace, ciliumDaemonSet); err != nil {
		return err
	}
	return kc.WaitForResource(ctx, kube.KindDaemonSet, ciliumNamespace, ciliumDaemonSet, ciliumDaemonSetRolledOut, ciliumRolloutTimeout)
}

// ciliumDaemonSetRolledOut reports true once the Cilium DaemonSet restart has fully
// rolled out: the controller has observed the latest spec and every scheduled pod
// is updated and ready. Transient get errors keep it polling.
func ciliumDaemonSetRolledOut(obj *unstructured.Unstructured, err error) (bool, error) {
	if err != nil || obj == nil {
		return false, nil
	}
	generation, _, _ := unstructured.NestedInt64(obj.Object, "metadata", "generation")
	observed, _, _ := unstructured.NestedInt64(obj.Object, "status", "observedGeneration")
	desired, _, _ := unstructured.NestedInt64(obj.Object, "status", "desiredNumberScheduled")
	updated, _, _ := unstructured.NestedInt64(obj.Object, "status", "updatedNumberScheduled")
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "numberReady")
	return observed >= generation && desired > 0 && updated == desired && ready == desired, nil
}
