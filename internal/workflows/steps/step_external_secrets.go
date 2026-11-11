// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/release"
)

const (
	PreCheckExternalSecretsStepId  = "precheck-external-secrets"
	SetupExternalSecretsStepId     = "setup-external-secrets"
	InstallExternalSecretsStepId   = "install-external-secrets"
	IsExternalSecretsReadyStepId   = "is-external-secrets-ready"
	TeardownExternalSecretsStepId  = "teardown-external-secrets"
	UninstallExternalSecretsStepId = "uninstall-external-secrets"

	// esoChartName identifies an ESO release whatever it was named. The catalog's
	// spec.Chart is alias-prefixed ("external-secrets/external-secrets"), so it
	// cannot be compared to Chart.Metadata.Name directly.
	esoChartName = "external-secrets"
)

// SetupExternalSecrets returns a workflow builder that installs the External
// Secrets Operator at the catalog default version. An empty namespace selects
// the catalog default namespace. force lets the install recover from a partial
// uninstall, and adds a last step that finishes that recovery.
func SetupExternalSecrets(namespace string, force bool) *automa.WorkflowBuilder {
	spec := chartSpec("external-secrets")
	if namespace != "" {
		spec.Namespace = namespace
	}

	steps := []automa.Builder{
		preCheckExternalSecrets(spec, true, force, defaultESOClusterReader),
		installExternalSecrets(spec, force, defaultESOClusterReader),
	}
	if force {
		// Before readiness: the controller may never be ready while CRDs are missing.
		steps = append(steps, finishESORecovery(spec, defaultESOClusterReader, helm.DefaultTimeout))
	}
	steps = append(steps, isExternalSecretsReady(spec))

	return automa.NewWorkflowBuilder().WithId(SetupExternalSecretsStepId).Steps(steps...).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up External Secrets Operator")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup External Secrets Operator")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "External Secrets Operator setup successfully")
		})
}

// TeardownExternalSecrets returns a workflow builder that uninstalls the External
// Secrets Operator. An empty namespace selects the catalog default namespace.
func TeardownExternalSecrets(namespace string) *automa.WorkflowBuilder {
	spec := chartSpec("external-secrets")
	if namespace != "" {
		spec.Namespace = namespace
	}

	return automa.NewWorkflowBuilder().WithId(TeardownExternalSecretsStepId).Steps(
		preCheckExternalSecrets(spec, false, false, defaultESOClusterReader),
		uninstallExternalSecrets(spec, defaultESOClusterReader),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Tearing down External Secrets Operator")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to tear down External Secrets Operator")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "External Secrets Operator torn down successfully")
		})
}

// checkClusterReachable fails when the cluster is unreachable, so that surfaces
// here instead of as a cryptic Helm error from the first IsInstalled call.
func checkClusterReachable(reachable bool) error {
	if !reachable {
		return errx.Decorate(
			errorx.IllegalState.New("Kubernetes cluster is not reachable"),
			reasons.PreconditionNotMet,
			"Install or verify the cluster first:",
			"  solo-provisioner kube cluster install",
			"Confirm the API server is reachable:",
			"  kubectl cluster-info",
		)
	}
	return nil
}

// isESORelease reports whether rel installs the ESO chart, whatever the release
// was named. The release name is only a fallback for a release whose chart
// metadata is missing.
func isESORelease(rel *release.Release, spec *helmChartSpec) bool {
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		return rel.Chart.Metadata.Name == esoChartName
	}
	return rel.Name == spec.Release
}

// checkESOSingleton fails when ESO cannot be installed as spec.Release in
// spec.Namespace. ESO's CRDs are cluster-scoped, so only one instance can exist:
// a second one collides on Helm ownership metadata and leaves a half-created
// namespace behind.
//
// ListAll is used rather than IsInstalled because IsInstalled counts only
// deployed releases, so it cannot see a stalled one.
func checkESOSingleton(hm helm.Manager, spec *helmChartSpec) error {
	releases, err := hm.ListAll()
	if err != nil {
		return errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to list Helm releases"),
			reasons.PreconditionNotMet,
			"Verify the cluster is reachable: kubectl cluster-info",
		)
	}

	// The foreign-chart case hints raw "helm uninstall": "eso operator uninstall"
	// removes only ESO releases.
	for _, rel := range releases {
		if rel == nil {
			continue
		}
		atTarget := rel.Name == spec.Release && rel.Namespace == spec.Namespace

		switch {
		case isESORelease(rel, spec) && !atTarget:
			return errx.Decorate(
				errorx.IllegalState.New(
					"External Secrets Operator is already installed as release %q in namespace %q; its CRDs are cluster-scoped, so only one instance can exist",
					rel.Name, rel.Namespace),
				reasons.PreconditionNotMet,
				"Use the existing installation, or remove it first:",
				fmt.Sprintf("  sudo solo-provisioner eso operator uninstall --namespace %s", rel.Namespace),
			)

		case !isESORelease(rel, spec) && atTarget:
			// Left alone, installESOChart's deployed-only IsInstalled would read
			// this as ESO and report a no-op install.
			return errx.Decorate(
				errorx.IllegalState.New(
					"release %q in namespace %q is chart %q, not the External Secrets Operator",
					rel.Name, rel.Namespace, chartName(rel)),
				reasons.PreconditionNotMet,
				"Install into a different namespace, or remove the conflicting release:",
				fmt.Sprintf("  helm uninstall %s -n %s", rel.Name, rel.Namespace),
			)

		case atTarget && releaseStatus(rel) != release.StatusDeployed:
			return errx.Decorate(
				errorx.IllegalState.New(
					"External Secrets Operator release %q in namespace %q is %s, not deployed",
					rel.Name, rel.Namespace, releaseStatus(rel)),
				reasons.PreconditionNotMet,
				"Remove the stalled release, then retry:",
				fmt.Sprintf("  sudo solo-provisioner eso operator uninstall --namespace %s", rel.Namespace),
				"If the uninstall refuses because objects still hold finalizers, follow its hints first",
			)
		}
	}

	return nil
}

// esoCRDs are the CRDs every ESO install creates. All are checked, so an
// install that left only some of them behind is still found. The finalized
// kinds' CRDs are among them, so any CRD a partial uninstall can leave
// Terminating is seen.
var esoCRDs = []string{
	"externalsecrets.external-secrets.io",
	"clusterexternalsecrets.external-secrets.io",
	"secretstores.external-secrets.io",
	"clustersecretstores.external-secrets.io",
	"pushsecrets.external-secrets.io",
	"generatorstates.generators.external-secrets.io",
}

// esoRequiredCRDs are the esoCRDs every install creates. The others are optional
// in the chart (crds.createClusterExternalSecret, createClusterSecretStore,
// createPushSecret), so a healthy ESO can lack them.
var esoRequiredCRDs = []string{
	"externalsecrets.external-secrets.io",
	"secretstores.external-secrets.io",
	"generatorstates.generators.external-secrets.io",
}

// checkOrphanedESOCRDs fails when ESO CRDs exist but no Helm release of the ESO
// chart does. Helm adopts existing resources that carry its ownership labels for
// the same release name and namespace, so an install over an ESO whose release
// record was lost would replace its CRDs and workloads with the catalog default.
// When every CRD found is terminating, a previous uninstall stopped partway, and
// checkPartialUninstallRecovery decides. When an ESO release exists,
// checkInstalledESOCRDs checks it still has its CRDs.
//
// The warning is for the operator, set only when a forced install goes ahead.
func checkOrphanedESOCRDs(ctx context.Context, hm helm.Manager, k esoClusterReader, spec *helmChartSpec, force bool) (warning string, err error) {
	releases, err := hm.ListAll()
	if err != nil {
		return "", errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to list Helm releases"),
			reasons.PreconditionNotMet,
			"Verify the cluster is reachable: kubectl cluster-info",
		)
	}
	for _, rel := range releases {
		if rel != nil && isESORelease(rel, spec) {
			return checkInstalledESOCRDs(ctx, k, spec, force)
		}
	}

	var found []string
	allTerminating := true
	for _, name := range esoCRDs {
		exists, terminating, err := k.CRDState(ctx, name)
		if err != nil {
			return "", errx.Decorate(
				errorx.ExternalError.Wrap(err, "failed to check for existing External Secrets Operator CRDs"),
				reasons.K8sAPIFailed,
				"Verify the cluster is reachable: kubectl cluster-info",
				"Check the CRD: kubectl get crd "+name,
			)
		}
		if exists {
			found = append(found, name)
			allTerminating = allTerminating && terminating
		}
	}
	if len(found) == 0 {
		return "", nil
	}
	if allTerminating {
		return checkPartialUninstallRecovery(ctx, k, spec, found, force)
	}

	hints := []string{
		"Find what manages them: kubectl get crd " + strings.Join(found, " ") + " -o yaml | grep -E 'managed-by|release-name|release-namespace'",
		"If another tool (for example Argo CD) manages External Secrets Operator, keep managing it there",
		// Only the CRDs found: kubectl fails on a kind whose CRD is gone.
		"If they are left over from a removed installation, see what still uses them: kubectl get " + strings.Join(found, ",") + " -A",
	}
	if force {
		hints = append(hints, "--force only applies after a partial uninstall, when every External Secrets Operator CRD is being deleted; see "+esoRecoveryDoc)
	}
	return "", errx.Decorate(
		errorx.IllegalState.New(
			"External Secrets Operator CRDs already exist (%s) but no Helm release of the External Secrets Operator does; installing would take over that existing installation, so nothing was changed",
			strings.Join(found, ", ")),
		reasons.PreconditionNotMet,
		hints...,
	)
}

// chartName reports rel's chart name, or "unknown" when its metadata is missing.
func chartName(rel *release.Release) string {
	if rel.Chart == nil || rel.Chart.Metadata == nil {
		return "unknown"
	}
	return rel.Chart.Metadata.Name
}

// releaseStatus reports rel's status, or unknown when its info is missing.
func releaseStatus(rel *release.Release) release.Status {
	if rel.Info == nil {
		return release.StatusUnknown
	}
	return rel.Info.Status
}

// preCheckExternalSecrets gates both ESO workflows, failing before Helm installs anything
// rather than deep inside one. The install guards are install-only: an uninstall
// should remove ESO from whichever namespace it actually occupies.
func preCheckExternalSecrets(spec *helmChartSpec, install, force bool, provider esoClusterReaderProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(PreCheckExternalSecretsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			_, reachable := kube.ProbeCluster()
			if err := checkClusterReachable(reachable); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if install {
				hm, err := newHelmManager()
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.InternalError.Wrap(err, "failed to initialise Helm manager"),
						reasons.Internal,
					)))
				}
				if err := checkESOSingleton(hm, spec); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
				k, err := resolveESOClusterReader(ctx, provider)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
				warning, err := checkOrphanedESOCRDs(ctx, hm, k, spec, force)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
				if warning != "" {
					return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
						models.ReportMetaWarning: warning,
					}))
				}
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking prerequisites")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Prerequisite checks failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Prerequisites satisfied")
		})
}

// installESOChart runs the idempotent ESO Helm install and reports whether it
// installed (false = already present). Extracted from the step so it can be
// unit-tested with a mock helm.Manager. A recovery over a partial uninstall
// installs without Helm's wait and atomic rollback: the adopted CRDs are meant
// to finish deleting, which Helm's wait would read as a failure and roll back.
// The readiness and recovery steps do the waiting instead.
func installESOChart(ctx context.Context, hm helm.Manager, spec *helmChartSpec, recovering bool) (bool, error) {
	isInstalled, err := hm.IsInstalled(spec.Release, spec.Namespace)
	if err != nil {
		return false, err
	}
	if isInstalled {
		return false, nil
	}

	if _, err := hm.AddRepo(spec.RepoAlias, spec.Repo, helm.RepoAddOptions{}); err != nil {
		return false, err
	}

	localChart, err := hm.PullAndVerify(ctx, chartDownloadsDir(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum)
	if err != nil {
		return false, err
	}

	helmValues := []string{
		"installCRDs=true",
		"webhook.port=9443",
	}

	if _, err := hm.InstallChart(
		ctx,
		spec.Release,
		localChart,
		"",
		spec.Namespace,
		helm.InstallChartOptions{
			ValueOpts: &values.Options{
				Values: helmValues,
			},
			CreateNamespace: true,
			Atomic:          !recovering,
			Wait:            !recovering,
			Timeout:         helm.DefaultTimeout,
		},
	); err != nil {
		return false, err
	}

	return true, nil
}

func installExternalSecrets(spec *helmChartSpec, force bool, provider esoClusterReaderProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(InstallExternalSecretsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := newHelmManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			recovering, err := isESORecovery(ctx, force, provider)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			installed, err := installESOChart(ctx, hm, spec, recovering)
			if err != nil {
				if recovering {
					err = errx.Decorate(err, reasons.PreconditionNotMet,
						"Check what is left: kubectl get crd | grep external-secrets",
						"Remove the failed release, then run the recovery again:",
						fmt.Sprintf("  sudo solo-provisioner eso operator uninstall --namespace %s", spec.Namespace),
						fmt.Sprintf("  sudo solo-provisioner eso operator install --force --namespace %s", spec.Namespace))
				}
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if !installed {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("External Secrets Operator is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			meta[InstalledByThisStep] = "true"
			stp.State().Local().Set(InstalledByThisStep, true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if v, _ := stp.State().Local().Bool(InstalledByThisStep); v == false {
				return automa.StepSkippedReport(stp.Id())
			}

			hm, err := newHelmManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// The same refusal as eso operator uninstall: a recovery install's
			// rollback would otherwise remove the controller the old objects wait on.
			k, err := resolveESOClusterReader(ctx, provider)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if err := checkRollbackLeavesNoFinalizedObjects(ctx, k, spec); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = hm.UninstallChart(spec.Release, spec.Namespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing External Secrets Operator")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install External Secrets Operator")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "External Secrets Operator installed successfully")
		})
}

// resolveESORelease reports the ESO release occupying spec.Namespace, or nil when
// that namespace holds none. List is used rather than IsInstalled or GetRelease:
// both key on spec.Release, which the catalog fixes, and IsInstalled counts only
// deployed releases. The search never leaves spec.Namespace — uninstalling ESO
// deletes cluster-scoped CRDs.
func resolveESORelease(hm helm.Manager, spec *helmChartSpec) (*release.Release, error) {
	releases, err := hm.List(spec.Namespace, false)
	if err != nil {
		return nil, errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to list Helm releases in namespace %q", spec.Namespace),
			reasons.PreconditionNotMet,
			"Verify the cluster is reachable: kubectl cluster-info",
		)
	}

	// squatter holds the catalog release name but is not ESO. It cannot also be a
	// candidate: isESORelease's name fallback classifies that case as ESO.
	var candidates []*release.Release
	var squatter *release.Release
	for _, rel := range releases {
		if rel == nil {
			continue
		}
		switch {
		case isESORelease(rel, spec):
			candidates = append(candidates, rel)
		case rel.Name == spec.Release:
			squatter = rel
		}
	}

	switch len(candidates) {
	case 0:
		if squatter != nil {
			// Not ESO, so not ours to remove.
			logx.As().Warn().
				Str("release", squatter.Name).
				Str("namespace", squatter.Namespace).
				Str("chart", chartName(squatter)).
				Msg("Release holding the External Secrets Operator release name is a different chart, leaving it alone")
		}
		return nil, nil

	case 1:
		return candidates[0], nil

	default:
		// An uninstalled record owns nothing, so it must never outrank one that does.
		var occupying []*release.Release
		for _, rel := range candidates {
			if releaseStatus(rel) != release.StatusUninstalled {
				occupying = append(occupying, rel)
			}
		}

		// All leftovers: the catalog name is the one this command owns.
		choices := occupying
		if len(choices) == 0 {
			choices = candidates
		}
		if len(choices) == 1 {
			return choices[0], nil
		}
		for _, rel := range choices {
			if rel.Name == spec.Release {
				return rel, nil
			}
		}

		// Two live ESO releases collide on namespaced resources, so this should be unreachable.
		return nil, errx.Decorate(
			errorx.IllegalState.New(
				"namespace %q holds %d External Secrets Operator releases (%s); cannot choose one to uninstall",
				spec.Namespace, len(choices), strings.Join(releaseNames(choices), ", ")),
			reasons.PreconditionNotMet,
			"Remove the one you want by name:",
			fmt.Sprintf("  helm uninstall <release> -n %s", spec.Namespace),
		)
	}
}

// releaseNames lists rels by name, for an error message.
func releaseNames(rels []*release.Release) []string {
	names := make([]string, 0, len(rels))
	for _, rel := range rels {
		names = append(names, rel.Name)
	}
	return names
}

// uninstallESOChart removes the ESO release occupying spec.Namespace and reports
// which one it removed; a nil release with a nil error means the namespace held
// none. It refuses while finalized ESO objects exist. Extracted from the step so
// it can be unit-tested with a mock helm.Manager and a fake reader.
func uninstallESOChart(ctx context.Context, hm helm.Manager, k esoClusterReader, spec *helmChartSpec) (*release.Release, error) {
	rel, err := resolveESORelease(hm, spec)
	if err != nil || rel == nil {
		return nil, err
	}

	if err := checkNoFinalizedESOObjects(ctx, k, rel, spec); err != nil {
		return nil, err
	}

	// rel.Name, not spec.Release: the release found here may carry any name.
	// spec.Namespace, not rel.Namespace: the uninstall must not leave the namespace asked for.
	if err := hm.UninstallChart(rel.Name, spec.Namespace); err != nil {
		return nil, decorateESOUninstallFailure(ctx, hm, k, rel, spec, err)
	}
	return rel, nil
}

func uninstallExternalSecrets(spec *helmChartSpec, provider esoClusterReaderProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(UninstallExternalSecretsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := newHelmManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.InternalError.Wrap(err, "failed to initialise Helm manager"),
					reasons.Internal,
				)))
			}

			k, err := resolveESOClusterReader(ctx, provider)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Already decorated at each failure site; re-wrapping would overwrite it.
			rel, err := uninstallESOChart(ctx, hm, k, spec)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if rel == nil {
				l.Info().Msg("External Secrets Operator is not installed, skipping uninstallation")
				return automa.StepSkippedReport(stp.Id(), automa.WithMetadata(uninstallSkipMetadata(ctx, k)))
			}

			l.Info().
				Str("release", rel.Name).
				Str("namespace", rel.Namespace).
				Str("priorStatus", string(releaseStatus(rel))).
				Msg("Uninstalled External Secrets Operator release")

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				"uninstalled": "true",
				"release":     rel.Name,
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Uninstalling External Secrets Operator")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to uninstall External Secrets Operator")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "External Secrets Operator uninstalled successfully")
		})
}

func isExternalSecretsReady(spec *helmChartSpec) automa.Builder {
	return automa.NewStepBuilder().WithId(IsExternalSecretsReadyStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			// Wait for external-secrets pods to be ready
			err = k.WaitForResources(ctx, kube.KindPod, spec.Namespace, kube.IsPodReady, 5*time.Minute, kube.WaitOptions{NamePrefix: "external-secrets"})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[IsReady] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Verifying External Secrets Operator readiness")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "External Secrets Operator is not ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "External Secrets Operator is ready")
		})
}
