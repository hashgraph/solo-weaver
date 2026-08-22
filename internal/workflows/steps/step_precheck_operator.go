// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/joomcode/errorx"
)

const (
	PrecheckOperatorCRDsStepId      = "precheck-operator-crds"
	PrecheckOperatorRunningStepId   = "precheck-operator-running"
	PrecheckOperatorVersionStepId   = "precheck-operator-version"

	operatorDeploymentName      = "solo-operator-controller-manager"
	operatorChartName           = "solo-operator"
	operatorNamespace           = "solo-operator"
	operatorReleaseName         = "solo-operator"
)

// ConsensusNodeCRDs lists all solo-operator CRDs required for the consensus
// node lifecycle (install, genesis, upgrade).
var ConsensusNodeCRDs = []string{
	"orbits",
	"consensuscapsules",
	"networkgeneses",
	"log4j2configs",
	"nodesettings",
	"applicationproperties",
	"applicationoverrideproperties",
	"apipermissionproperties",
	"bootstrapproperties",
	"nodepropertiesconfigs",
	"feeschedules",
	"simplefeesschedules",
	"throttlesconfigs",
	"blocknodesconfigs",
	"networkupgradeprepares",
	"networkupgradefreezes",
	"networkupgradefreezeaborts",
	"networkupgradeexecutes",
}

// PrecheckOperatorCRDs verifies that the Kubernetes cluster is reachable and
// that the given solo-operator CRDs are installed. Each component workflow
// passes in the CRD names it needs (e.g. "orbits", "consensuscapsules").
func PrecheckOperatorCRDs(crdNames ...string) automa.Builder {
	return automa.NewStepBuilder().WithId(PrecheckOperatorCRDsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster")))
			}

			logx.As().Info().Msg("Cluster reachable")

			crdApiVersion := "apiextensions.k8s.io/v1"
			for _, name := range crdNames {
				fqdn := name + "." + kube.SoloOperatorGroup
				exists, err := kc.ResourceExists(ctx, crdApiVersion, "CustomResourceDefinition", "", fqdn)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(
						errorx.IllegalState.Wrap(err, "failed to check CRD %s", fqdn)))
				}
				if !exists {
					return automa.StepFailureReport(stp.Id(), automa.WithError(
						errorx.IllegalState.New("CRD %s not found — is solo-operator installed?", fqdn)))
				}
			}

			logx.As().Info().Msg("Required solo-operator CRDs present")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking cluster and solo-operator CRDs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Cluster precheck failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cluster precheck passed")
		})
}

// PrecheckOperatorRunning verifies that the solo-operator controller-manager
// Deployment exists and has at least one available replica.
func PrecheckOperatorRunning() automa.Builder {
	return automa.NewStepBuilder().WithId(PrecheckOperatorRunningStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster")))
			}

			exists, err := kc.ResourceExists(ctx, "apps/v1", "Deployment",
				operatorNamespace, operatorDeploymentName)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.IllegalState.Wrap(err, "failed to check operator deployment")))
			}
			if !exists {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.IllegalState.New(
						"solo-operator deployment %s/%s not found — run 'solo-provisioner operator install' first",
						operatorNamespace, operatorDeploymentName)))
			}

			availableStr, err := kc.GetResourceNestedString(ctx, "apps/v1", "Deployment",
				operatorNamespace, operatorDeploymentName,
				"status", "availableReplicas")
			if err != nil || availableStr == "" || availableStr == "0" {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.IllegalState.New(
						"solo-operator deployment %s/%s has no available replicas",
						operatorNamespace, operatorDeploymentName)))
			}

			logx.As().Info().
				Str("deployment", operatorDeploymentName).
				Str("namespace", operatorNamespace).
				Msg("Solo-operator is running")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking solo-operator deployment")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Solo-operator not running")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo-operator is running")
		})
}

// PrecheckOperatorVersion verifies that the installed solo-operator Helm chart
// version matches the version pinned in the infrastructure catalog. Fails if
// there is a mismatch so the user can upgrade explicitly.
func PrecheckOperatorVersion() automa.Builder {
	spec := chartSpec(operatorChartName)
	return automa.NewStepBuilder().WithId(PrecheckOperatorVersionStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hm, err := newHelmManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			rel, err := hm.GetRelease(operatorReleaseName, operatorNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.IllegalState.Wrap(err, "failed to get solo-operator Helm release")))
			}

			installedVersion := rel.Chart.Metadata.Version
			expectedVersion := spec.Version

			if installedVersion != expectedVersion {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.IllegalState.New(
						"solo-operator version mismatch: installed %s, expected %s — upgrade the operator before proceeding",
						installedVersion, expectedVersion)))
			}

			logx.As().Info().
				Str("version", installedVersion).
				Msg("Solo-operator version matches catalog")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				"version": installedVersion,
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Checking solo-operator version (expected %s)", spec.Version))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Solo-operator version mismatch")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo-operator version verified")
		})
}
