// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	InstallSoloOperatorStepId   = "install-solo-operator"
	UninstallSoloOperatorStepId = "uninstall-solo-operator"
)

// ociRegistryHost extracts the registry host from an OCI chart reference
// (e.g. "oci://ghcr.io/hashgraph/solo-operator/solo-operator-chart" -> "ghcr.io")
// for use in login hints. Returns the whole ref if it has no host segment.
func ociRegistryHost(chartRef string) string {
	rest := strings.TrimPrefix(chartRef, "oci://")
	if i := strings.IndexByte(rest, '/'); i != -1 {
		return rest[:i]
	}
	return rest
}

// InstallSoloOperator installs the solo-operator Helm chart. imagePullSecret, when
// non-empty, is passed to the chart as imagePullSecrets[0].name so the operator's
// pods can pull images from a private registry — the named docker-registry secret
// must already exist in the operator namespace (this step does not create it).
func InstallSoloOperator(imagePullSecret string, allowUpgrade ...bool) automa.Builder {
	upgrade := len(allowUpgrade) > 0 && allowUpgrade[0]
	spec := chartSpec("solo-operator")
	return automa.NewStepBuilder().WithId(InstallSoloOperatorStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := newHelmManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(spec.Release, spec.Namespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				rel, err := hm.GetRelease(spec.Release, spec.Namespace)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}

				installedVersion := rel.Chart.Metadata.Version
				if installedVersion == spec.Version {
					meta[AlreadyInstalled] = "true"
					l.Info().Str("version", installedVersion).
						Msg("Solo Operator is already installed at the expected version, skipping")
					return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
				}

				if !upgrade {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.New(
							"solo-operator version mismatch: installed %s, expected %s — re-run with --upgrade-operator to upgrade",
							installedVersion, spec.Version),
						reasons.PreconditionNotMet,
						"Re-run with --upgrade-operator to upgrade solo-operator to the expected version")))
				}

				l.Info().
					Str("installed", installedVersion).
					Str("expected", spec.Version).
					Msg("Solo Operator version mismatch, upgrading")
			}

			localChart, err := hm.PullAndVerify(ctx, chartDownloadsDir(), spec.Chart, spec.Version, spec.Algorithm, spec.Checksum)
			if err != nil {
				if errorx.IsOfType(err, helm.ErrChecksumMismatch) {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						err,
						reasons.PreconditionNotMet,
						"Confirm the chart is pulled from the official registry over a trusted network — a proxy rewriting responses can change the digest",
						"If it persists the pinned checksum is stale (the chart was re-published at the same version) — regenerate it with `task chart-checksums` and update pkg/software/infrastructure-catalog.yaml, or report it to the solo-weaver maintainers")))
				}
				// A pull failure on a private registry surfaces as an opaque auth/pull
				// error; point the operator at the login step (public charts need none).
				host := ociRegistryHost(spec.Chart)
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					err,
					reasons.PreconditionNotMet,
					fmt.Sprintf("If %s is a private registry, authenticate first: helm registry login %s -u <user> -p <token>", host, host),
					fmt.Sprintf("On 401/403 (denied), the token lacks access: it needs the read:packages scope and, for an SSO org, must be SSO-authorized to read %s's packages", host),
					fmt.Sprintf("Verify network connectivity to %s and that chart %q version %q exists", host, spec.Chart, spec.Version))))
			}

			// A private registry needs an image-pull secret on the operator's pods.
			// Pass it as the chart's imagePullSecrets[0].name; the named secret must
			// already exist in spec.Namespace (created out-of-band before this step).
			var valueOpts *values.Options
			if imagePullSecret != "" {
				valueOpts = &values.Options{
					Values: []string{fmt.Sprintf("imagePullSecrets[0].name=%s", imagePullSecret)},
				}
			}

			_, err = hm.DeployChart(
				ctx,
				spec.Release,
				localChart,
				"",
				spec.Namespace,
				helm.DeployChartOptions{
					ValueOpts:       valueOpts,
					CreateNamespace: true,
					Atomic:          true,
					Wait:            true,
					Timeout:         helm.DefaultTimeout,
				},
			)
			if err != nil {
				if isHelmOperationInProgress(err) {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						err,
						reasons.PreconditionNotMet,
						"A previous install/upgrade left the solo-operator release in a pending state (Helm reports \"another operation ... in progress\").",
						fmt.Sprintf("Clear it and retry: 'sudo solo-provisioner kube operator uninstall' (removes a pending/failed release), or delete the release records: kubectl -n %s delete secret -l owner=helm,name=%s", spec.Namespace, spec.Release))))
				}
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					err,
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and that the operator namespace and its image-pull secret are in place",
					fmt.Sprintf("Inspect the operator's pods and events: kubectl -n %s get pods,events", spec.Namespace))))
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
			notify.As().StepStart(ctx, stp, "Installing Solo Operator")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install Solo Operator")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Operator installed successfully")
		})
}

// isHelmOperationInProgress reports whether err is Helm's "another operation
// (install/upgrade/rollback) is in progress" — a prior install/upgrade that left the
// release in a pending state, which must be cleared before a retry can succeed.
func isHelmOperationInProgress(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "another operation") && strings.Contains(msg, "in progress")
}

// UninstallSoloOperator removes the solo-operator Helm release. It is a no-op (skip)
// only when no release record exists at all; a release in any state — deployed,
// pending-install/upgrade/rollback, or failed — is removed, so `kube operator
// uninstall` is idempotent AND recovers a release stuck mid-install.
func UninstallSoloOperator() automa.Builder {
	spec := chartSpec("solo-operator")
	return automa.NewStepBuilder().WithId(UninstallSoloOperatorStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hm, err := newHelmManager()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to initialise Helm client"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			// Gate on whether a release RECORD exists (any state), not on the
			// deployed-only IsInstalled: a release stuck in pending-*/failed is exactly
			// what needs clearing, and skipping it would leave a broken release that
			// blocks the next install with "another operation ... in progress".
			if _, err := hm.GetRelease(spec.Release, spec.Namespace); err != nil {
				if errorx.IsOfType(err, helm.ErrNotFound) {
					return automa.StepSkippedReport(stp.Id())
				}
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to read solo-operator Helm release"),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and that your kubeconfig has access to the operator namespace")))
			}

			if err := hm.UninstallChart(spec.Release, spec.Namespace); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to uninstall solo-operator Helm release"),
					reasons.PreconditionNotMet,
					fmt.Sprintf("Retry; if it persists, clear the release records manually: kubectl -n %s delete secret -l owner=helm,name=%s", spec.Namespace, spec.Release))))
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Uninstalling Solo Operator")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to uninstall Solo Operator")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Solo Operator uninstalled successfully")
		})
}
