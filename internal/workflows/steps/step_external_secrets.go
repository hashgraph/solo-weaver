// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	SetupExternalSecretsStepId     = "setup-external-secrets"
	InstallExternalSecretsStepId   = "install-external-secrets"
	IsExternalSecretsReadyStepId   = "is-external-secrets-ready"
	TeardownExternalSecretsStepId  = "teardown-external-secrets"
	UninstallExternalSecretsStepId = "uninstall-external-secrets"
)

// ESOInstallOptions parameterizes the External Secrets Operator install workflow.
// Empty fields select the catalog default namespace and version.
type ESOInstallOptions struct {
	Namespace string
	Version   string
}

// SetupExternalSecrets returns a workflow builder that installs the External Secrets Operator.
func SetupExternalSecrets(opts ESOInstallOptions) (*automa.WorkflowBuilder, error) {
	spec, err := resolveCatalogChartVersion("external-secrets", opts.Version)
	if err != nil {
		return nil, err
	}
	if opts.Namespace != "" {
		spec.Namespace = opts.Namespace
	}

	return automa.NewWorkflowBuilder().WithId(SetupExternalSecretsStepId).Steps(
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
		}), nil
}

// TeardownExternalSecrets returns a workflow builder that uninstalls the External
// Secrets Operator. An empty namespace selects the catalog default namespace.
func TeardownExternalSecrets(namespace string) *automa.WorkflowBuilder {
	spec := chartSpec("external-secrets")
	if namespace != "" {
		spec.Namespace = namespace
	}

	return automa.NewWorkflowBuilder().WithId(TeardownExternalSecretsStepId).Steps(
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
