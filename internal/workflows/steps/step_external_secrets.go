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
// the catalog default namespace.
func SetupExternalSecrets(namespace string) *automa.WorkflowBuilder {
	spec := chartSpec("external-secrets")
	if namespace != "" {
		spec.Namespace = namespace
	}

	return automa.NewWorkflowBuilder().WithId(SetupExternalSecretsStepId).Steps(
		preCheckExternalSecrets(spec, true),
		installExternalSecrets(spec),
		isExternalSecretsReady(spec),
	).
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
		preCheckExternalSecrets(spec, false),
		uninstallExternalSecrets(spec),
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
// here instead of as a cryptic Helm error from the first IsInstalled call. probe
// is kube.ClusterExists in production; it is a parameter so this can be
// unit-tested without a cluster.
func checkClusterReachable(probe func() (bool, error)) error {
	// ClusterExists collapses every failure to (false, nil) today; this branch
	// exists because the signature allows an error.
	exists, err := probe()
	if err != nil {
		return errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to probe Kubernetes cluster reachability"),
			reasons.PreconditionNotMet,
			"Ensure the cluster is installed and its API server is reachable:",
			"  solo-provisioner kube cluster install",
			"  kubectl cluster-info",
		)
	}
	if !exists {
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

// isESORelease reports whether rel is an ESO installation. The chart name is
// authoritative; the release name is only a fallback for a release whose chart
// metadata is missing, so an unrelated chart named "external-secrets" does not
// match.
func isESORelease(rel *release.Release, spec *helmChartSpec) bool {
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		return rel.Chart.Metadata.Name == esoChartName
	}
	return rel.Name == spec.Release
}

// checkESOSingleton fails when ESO already exists somewhere that blocks this
// install. ESO's CRDs are cluster-scoped, so only one instance can exist: a
// second one collides on Helm ownership metadata and leaves a half-created
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

	for _, rel := range releases {
		if rel == nil || !isESORelease(rel, spec) {
			continue
		}

		// Both hints name raw "helm uninstall" rather than "eso operator
		// uninstall": that command gates on IsInstalled, which is deployed-only
		// and keyed to the catalog release name, so it silently skips a stalled
		// release or one installed under a different name.
		if rel.Namespace != spec.Namespace {
			return errx.Decorate(
				errorx.IllegalState.New(
					"External Secrets Operator already exists in namespace %q; its CRDs are cluster-scoped, so only one instance can exist",
					rel.Namespace),
				reasons.PreconditionNotMet,
				"Use the existing installation, or remove it first:",
				fmt.Sprintf("  helm uninstall %s -n %s", rel.Name, rel.Namespace),
			)
		}

		if rel.Info == nil || rel.Info.Status != release.StatusDeployed {
			return errx.Decorate(
				errorx.IllegalState.New(
					"External Secrets Operator release %q in namespace %q is not deployed (%s)",
					rel.Name, rel.Namespace, releaseStatus(rel)),
				reasons.PreconditionNotMet,
				"Remove the stalled release, then retry:",
				fmt.Sprintf("  helm uninstall %s -n %s", rel.Name, rel.Namespace),
			)
		}
	}

	return nil
}

func releaseStatus(rel *release.Release) release.Status {
	if rel.Info == nil {
		return release.StatusUnknown
	}
	return rel.Info.Status
}

// preCheckExternalSecrets gates both ESO workflows, failing before Helm installs anything
// rather than deep inside one. The singleton guard is install-only: an uninstall
// should remove ESO from whichever namespace it actually occupies.
func preCheckExternalSecrets(spec *helmChartSpec, singletonGuard bool) automa.Builder {
	return automa.NewStepBuilder().WithId(PreCheckExternalSecretsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := checkClusterReachable(kube.ClusterExists); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if singletonGuard {
				hm, err := newHelmManager()
				if err != nil {
					// Internal per docs/dev/error-handling.md: a bug, so no hints.
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.InternalError.Wrap(err, "failed to initialise Helm manager"),
						reasons.Internal,
					)))
				}
				if err := checkESOSingleton(hm, spec); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
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
// unit-tested with a mock helm.Manager.
func installESOChart(ctx context.Context, hm helm.Manager, spec *helmChartSpec) (bool, error) {
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
			Atomic:          true,
			Wait:            true,
			Timeout:         helm.DefaultTimeout,
		},
	); err != nil {
		return false, err
	}

	return true, nil
}

func installExternalSecrets(spec *helmChartSpec) automa.Builder {
	return automa.NewStepBuilder().WithId(InstallExternalSecretsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := newHelmManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			installed, err := installESOChart(ctx, hm, spec)
			if err != nil {
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

// uninstallESOChart runs the idempotent ESO Helm uninstall and reports whether it
// uninstalled (false = not installed). Extracted from the step so it can be
// unit-tested with a mock helm.Manager.
func uninstallESOChart(hm helm.Manager, spec *helmChartSpec) (bool, error) {
	isInstalled, err := hm.IsInstalled(spec.Release, spec.Namespace)
	if err != nil {
		return false, err
	}
	if !isInstalled {
		return false, nil
	}
	if err := hm.UninstallChart(spec.Release, spec.Namespace); err != nil {
		return false, err
	}
	return true, nil
}

func uninstallExternalSecrets(spec *helmChartSpec) automa.Builder {
	return automa.NewStepBuilder().WithId(UninstallExternalSecretsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := newHelmManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.InternalError.Wrap(err, "failed to initialise Helm manager").
						WithProperty(models.ErrPropertyResolution, []string{
							"Check the solo-provisioner logs for details: /opt/solo/weaver/logs/solo-provisioner.log",
						})))
			}

			uninstalled, err := uninstallESOChart(hm, spec)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.ExternalError.Wrap(err, "failed to uninstall External Secrets Operator release %q in namespace %q", spec.Release, spec.Namespace).
						WithProperty(models.ErrPropertyResolution, []string{
							"Verify the cluster is reachable: kubectl cluster-info",
							fmt.Sprintf("Check the release state: helm list -n %s", spec.Namespace),
							fmt.Sprintf("Remove it manually if needed: helm uninstall %s -n %s", spec.Release, spec.Namespace),
						})))
			}

			if !uninstalled {
				l.Info().Msg("External Secrets Operator is not installed, skipping uninstallation")
				return automa.StepSkippedReport(stp.Id())
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{"uninstalled": "true"}))
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
