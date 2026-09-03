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
)

const (
	InstallSoloOperatorStepId = "install-solo-operator"
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

func InstallSoloOperator(allowUpgrade ...bool) automa.Builder {
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

			_, err = hm.DeployChart(
				ctx,
				spec.Release,
				localChart,
				"",
				spec.Namespace,
				helm.DeployChartOptions{
					CreateNamespace: true,
					Atomic:          true,
					Wait:            true,
					Timeout:         helm.DefaultTimeout,
				},
			)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
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
