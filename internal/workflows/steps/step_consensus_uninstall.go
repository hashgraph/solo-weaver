// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ReleaseConsensusPVCsStepId     = "release-consensus-pvcs"
	DeleteConsensusCapsuleStepId   = "delete-consensus-capsule"
	DeleteConsensusConfigCRsStepId = "delete-consensus-config-crs"
	DeleteOrbitIfLastStepId        = "delete-orbit-if-last"

	// OrbitDeletedByThisStep records whether DeleteOrbitIfLast removed the Orbit.
	OrbitDeletedByThisStep = "orbitDeleted"
)

// Variables, not constants, so tests can shorten the wait.
var (
	capsuleDeletePollInterval = 2 * time.Second
	capsuleDeleteTimeout      = 5 * time.Minute
)

// consensusConfigCRKinds is the kind of each config CR that EnsureConfigCRs applies.
var consensusConfigCRKinds = []struct{ key, kind string }{
	{models.ConfigKeyLog4j2, "Log4j2Config"},
	{models.ConfigKeySettings, "NodeSettings"},
	{models.ConfigKeyAppProperties, "ApplicationProperties"},
	{models.ConfigKeyAppOverride, "ApplicationOverrideProperties"},
	{models.ConfigKeyApiPermission, "ApiPermissionProperties"},
	{models.ConfigKeyBootstrap, "BootstrapProperties"},
	{models.ConfigKeyNodeProperties, "NodePropertiesConfig"},
	{models.ConfigKeyFeeSchedules, "FeeSchedules"},
	{models.ConfigKeySimpleFeesSchedules, "SimpleFeesSchedules"},
	{models.ConfigKeyThrottles, "ThrottlesConfig"},
	{models.ConfigKeyBlockNodes, "BlockNodesConfig"},
}

// ReleaseConsensusPVCs removes the ConsensusCapsule ownerReference from the node's PVCs. The operator
// makes the capsule their controller, so the capsule delete would otherwise garbage-collect the node's data.
func ReleaseConsensusPVCs(inputs models.ConsensusNodeInputs, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(ReleaseConsensusPVCsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			capsuleName := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)
			capsuleKind := string(kube.KindConsensusCapsule)

			list, err := kc.List(ctx, kube.KindPVC, inputs.Namespace, kube.WaitOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to list PVCs in namespace %s", inputs.Namespace),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and RBAC for listing PersistentVolumeClaims",
					"Re-run uninstall; the capsule is not deleted until its PVCs are released")))
			}

			var released []string
			for i := range list.Items {
				name := list.Items[i].GetName()
				if !ownedBy(list.Items[i].GetOwnerReferences(), capsuleKind, capsuleName) {
					continue
				}
				if _, err := kc.RemoveOwnerReference(ctx, kube.KindPVC, inputs.Namespace, name, capsuleKind, capsuleName); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.ExternalError.Wrap(err, "failed to release PVC %s from ConsensusCapsule %s", name, capsuleName),
						reasons.PreconditionNotMet,
						"Verify RBAC for updating PersistentVolumeClaims",
						fmt.Sprintf("Inspect the owners: kubectl -n %s get pvc %s -o jsonpath='{.metadata.ownerReferences}'", inputs.Namespace, name),
						"Re-run uninstall; the capsule is not deleted until its PVCs are released")))
				}
				released = append(released, name)
			}

			logx.As().Info().Str("capsule", capsuleName).Strs("pvcs", released).Msg("PVCs released from ConsensusCapsule")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				"releasedCount": fmt.Sprintf("%d", len(released)),
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Keeping consensus node %d PVCs", inputs.NodeId))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to release PVCs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "PVCs released")
		})
}

// ownedBy reports whether refs name the given owner.
func ownedBy(refs []metav1.OwnerReference, kind, name string) bool {
	for _, r := range refs {
		if r.Kind == kind && r.Name == name {
			return true
		}
	}
	return false
}

// DeleteConsensusCapsule deletes the node's ConsensusCapsule and waits until the capsule object is gone.
// The wait does not cover the capsule's pods, which can still be stopping. An absent capsule is a success.
func DeleteConsensusCapsule(inputs models.ConsensusNodeInputs, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(DeleteConsensusCapsuleStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
			kind := string(kube.KindConsensusCapsule)
			capsuleName := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)

			// An absent CRD means its CRs are gone too, so treat it as already deleted.
			deleted, err := kc.DeleteResource(ctx, apiVersion, kind, inputs.Namespace, capsuleName)
			if err != nil && !kube.IsKindNotServed(err) {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to delete ConsensusCapsule %s", capsuleName),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and RBAC for deleting ConsensusCapsule resources",
					fmt.Sprintf("Inspect the capsule: kubectl -n %s get consensuscapsule %s", inputs.Namespace, capsuleName))))
			}
			if !deleted {
				logx.As().Info().Str("capsule", capsuleName).Msg("ConsensusCapsule already absent; nothing to delete")
				return automa.StepSuccessReport(stp.Id())
			}

			if err := waitForResourceGone(ctx, kc, apiVersion, kind, inputs.Namespace, capsuleName); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(err,
					reasons.PreconditionNotMet,
					fmt.Sprintf("Check what blocks the delete: kubectl -n %s get consensuscapsule %s -o jsonpath='{.metadata.finalizers}'", inputs.Namespace, capsuleName),
					"Check the solo-operator is running and inspect its logs",
					"Re-run uninstall once the capsule is gone")))
			}

			logx.As().Info().Str("capsule", capsuleName).Msg("ConsensusCapsule deleted")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Deleting consensus node %d capsule", inputs.NodeId))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to delete consensus capsule")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Consensus capsule deleted")
		})
}

// waitForResourceGone polls until the resource no longer exists, the timeout ends, or ctx is cancelled.
func waitForResourceGone(ctx context.Context, kc CapsuleKubeClient, apiVersion, kind, namespace, name string) error {
	deadline := time.Now().Add(capsuleDeleteTimeout)
	ticker := time.NewTicker(capsuleDeletePollInterval)
	defer ticker.Stop()

	for {
		absent, err := kube.ResourceAbsent(ctx, kc, apiVersion, kind, namespace, name)
		if err != nil {
			return errorx.ExternalError.Wrap(err, "failed to check %s %s", kind, name)
		}
		if absent {
			return nil
		}
		if time.Now().After(deadline) {
			return errorx.TimeoutElapsed.New("%s %s still exists %s after delete", kind, name, capsuleDeleteTimeout)
		}
		select {
		case <-ctx.Done():
			return errorx.IllegalState.Wrap(ctx.Err(), "cancelled while waiting for %s %s to be deleted", kind, name)
		case <-ticker.C:
		}
	}
}

// DeleteConsensusConfigCRs deletes the node's 11 config CRs. CRs that are already absent are skipped.
func DeleteConsensusConfigCRs(inputs models.ConsensusNodeInputs, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(DeleteConsensusConfigCRsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
			scope := models.ConsensusNodeScope(inputs.NodeId)

			deletedCount := 0
			for _, c := range consensusConfigCRKinds {
				name := models.ConsensusConfigCRName(scope, c.key)
				deleted, err := kc.DeleteResource(ctx, apiVersion, c.kind, inputs.Namespace, name)
				if err != nil && !kube.IsKindNotServed(err) {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.ExternalError.Wrap(err, "failed to delete %s %s", c.kind, name),
						reasons.PreconditionNotMet,
						"Verify cluster connectivity and RBAC for deleting solo-operator config CRs",
						"Re-run uninstall; config CRs that are already gone are skipped")))
				}
				if deleted {
					deletedCount++
				}
			}

			logx.As().Info().Str("scope", scope).Int("deleted", deletedCount).Msg("Config CRs deleted")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				"deletedCount": fmt.Sprintf("%d", deletedCount),
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Deleting config CRs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to delete config CRs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Config CRs deleted")
		})
}

// DeleteOrbitIfLast deletes the shared Orbit only when no other ConsensusCapsule remains in its
// namespace. The uninstalled capsule is excluded by name, in case it is still listed.
func DeleteOrbitIfLast(inputs models.ConsensusNodeInputs, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(DeleteOrbitIfLastStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			// Without the CRD the list path is NotFound and no capsule can exist.
			list, err := kc.List(ctx, kube.KindConsensusCapsule, inputs.Namespace, kube.WaitOptions{})
			if err != nil && !kerrors.IsNotFound(err) {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to list ConsensusCapsules in namespace %s", inputs.Namespace),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and RBAC for listing ConsensusCapsule resources")))
			}

			capsuleName := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)
			remaining := 0
			if list != nil {
				for i := range list.Items {
					if list.Items[i].GetName() != capsuleName {
						remaining++
					}
				}
			}

			if remaining > 0 {
				notify.As().StepDetail(ctx, stp, fmt.Sprintf("Orbit %s kept: %d other node(s) remain", inputs.OrbitName, remaining))
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
					OrbitDeletedByThisStep: "false",
				}))
			}

			deleted, err := kc.DeleteResource(ctx, kube.SoloOperatorGroup+"/"+kube.SoloOperatorVersion, string(kube.KindOrbit), "", inputs.OrbitName)
			if err != nil && !kube.IsKindNotServed(err) {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to delete Orbit %s", inputs.OrbitName),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and RBAC for deleting the cluster-scoped solo-operator Orbit CR",
					"Inspect the Orbit: kubectl get orbit "+inputs.OrbitName+" -o yaml")))
			}

			// The delete is asynchronous; the state prune after the workflow needs the Orbit gone.
			if deleted {
				if err := waitForResourceGone(ctx, kc, kube.SoloOperatorGroup+"/"+kube.SoloOperatorVersion, string(kube.KindOrbit), "", inputs.OrbitName); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(err,
						reasons.PreconditionNotMet,
						"Check what blocks the delete: kubectl get orbit "+inputs.OrbitName+" -o jsonpath='{.metadata.finalizers}'",
						"Check the solo-operator is running and inspect its logs",
						"Re-run uninstall once the Orbit is gone")))
				}
			}

			logx.As().Info().Str("orbit", inputs.OrbitName).Bool("deleted", deleted).Msg("No consensus nodes left on Orbit")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				OrbitDeletedByThisStep: fmt.Sprintf("%t", deleted),
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Deleting Orbit %s if no nodes remain", inputs.OrbitName))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to delete Orbit")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Orbit checked")
		})
}
