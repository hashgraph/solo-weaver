package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/logx"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
	"golang.hedera.com/solo-provisioner/pkg/helm"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	MetalLBNamespace          = "metallb-system"
	MetalLBRelease            = "metallb"
	MetalLBChart              = "metallb/metallb"
	MetalLBVersion            = "v0.15.2"
	MetalLBRepo               = "https://metallb.github.io/metallb"
	SetupMetalLBStepId        = "setup-metallb"
	InstallMetalLBStepId      = "install-metallb"
	DeployMetalLbConfigStepId = "deploy-metallb-config"
)

func SetupMetalLB() automa.Builder {
	return automa.NewWorkflowBuilder().WithId(SetupMetalLBStepId).Steps(
		installMetalLB(),
		configureMetalLB(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up MetalLB")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup MetalLB")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "MetalLB setup successfully")
		})
}

func installMetalLB() automa.Builder {
	return automa.NewStepBuilder().WithId(InstallMetalLBStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(MetalLBRelease, MetalLBNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("MetalLB is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			_, err = hm.AddRepo(MetalLBRelease, MetalLBRepo, helm.RepoAddOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			_, err = hm.InstallChart(
				ctx,
				MetalLBRelease,
				MetalLBChart,
				MetalLBVersion,
				MetalLBNamespace,
				helm.InstallChartOptions{
					ValueOpts: &values.Options{
						Values: []string{"speaker.frr.enabled=false"},
					},
					CreateNamespace: true,
					Atomic:          true,
					Wait:            true,
					Timeout:         helm.DefaultTimeout, // 5 minutes
				},
			)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[InstalledByThisStep] = "true"
			stp.State().Set(InstalledByThisStep, true)

			// wait for the release to be ready
			// Note: metallb-reaper container takes some time to be in running state
			// TODO: improve this by checking the actual status instead of sleeping
			err = Sleep(ctx, 60*time.Second)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if stp.State().Bool(InstalledByThisStep) == false {
				return automa.StepSkippedReport(stp.Id())
			}

			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = hm.UninstallChart(MetalLBRelease, MetalLBNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing MetalLB")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install MetalLB")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "MetalLB installed successfully")
		})
}

func configureMetalLB() *automa.StepBuilder {
	machineIp, err := runCmd(`ip route get 1 | head -1 | sed 's/^.*src \(.*\)$/\1/' | awk '{print $1}'`)
	if err != nil {
		machineIp = "0.0.0.0"
		logx.As().Warn().Err(err).Str("machine_ip", machineIp).
			Msg("failed to get machine IP, defaulting to 0.0.0.0")
	}

	configScript := fmt.Sprintf(
		`set -eo pipefail; cat <<EOF | %s/kubectl apply -f - 
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: private-address-pool
  namespace: metallb-system
spec:
  addresses:
    - 192.168.99.0/24
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: public-address-pool
  namespace: metallb-system
spec:
  addresses:
    - %s/32
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: primary-l2-advertisement
  namespace: metallb-system
spec:
  ipAddressPools:
    - private-address-pool
    - public-address-pool
EOF`, core.Paths().SandboxBinDir, machineIp)
	return automa_steps.BashScriptStep(DeployMetalLbConfigStepId, []string{
		configScript,
	}, "")
}
