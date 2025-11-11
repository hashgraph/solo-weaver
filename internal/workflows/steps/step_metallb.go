package steps

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/network"
	"golang.hedera.com/solo-provisioner/internal/templates"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
	"golang.hedera.com/solo-provisioner/pkg/helm"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	MetalLBNamespace             = "metallb-system"
	MetalLBRelease               = "metallb"
	MetalLBChart                 = "metallb/metallb"
	MetalLBVersion               = "0.15.2"
	MetalLBRepo                  = "https://metallb.github.io/metallb"
	SetupMetalLBStepId           = "setup-metallb"
	InstallMetalLBStepId         = "install-metallb"
	MetalLBTemplatePath          = "files/metallb/metallb.yaml"
	ConfigureMetalLbConfigStepId = "configure-metallb-config"
	PrepareMetalLbConfigStepId   = "prepare-metallb-config"
	DeployMetalLbConfigStepId    = "deploy-metallb-config"
)

var (
	// we create a temp file for metallb configuration
	metalLBConfigFilePath = path.Join(core.Paths().TempDir, "metallb-config.yaml")
)

func SetupMetalLB() automa.Builder {
	return automa.NewWorkflowBuilder().WithId(SetupMetalLBStepId).Steps(
		installMetalLB(),
		configureMetalLB(metalLBConfigFilePath),
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

func configureMetalLB(configFilePath string) automa.Builder {
	return automa.NewWorkflowBuilder().WithId(ConfigureMetalLbConfigStepId).
		Steps(
			prepareMetalLBConfigFile(configFilePath),
			deployMetalLBConfig(configFilePath),
		)
}

func prepareMetalLBConfigFile(configFilePath string) automa.Builder {
	return automa.NewStepBuilder().WithId(PrepareMetalLbConfigStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			machineIp, err := network.GetMachineIP()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			tmplData := templates.MetallbData{
				MachineIP: machineIp,
			}

			rendered, err := templates.Render(MetalLBTemplatePath, tmplData)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = os.WriteFile(configFilePath, []byte(rendered), 0644)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[ConfiguredByThisStep] = "true"
			meta[ConfigurationFile] = configFilePath

			stp.State().Set(ConfiguredByThisStep, true)
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Configuring MetalLB")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to configure MetalLB")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "MetalLB configured successfully")
		})
}

func deployMetalLBConfig(configFilePath string) automa.Builder {
	return automa.NewStepBuilder().WithId(DeployMetalLbConfigStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			// TODO: use kubernetes client-go instead of kubectl command
			cmd := fmt.Sprintf("%s/kubectl apply -f %s", core.Paths().SandboxBinDir, configFilePath)
			_, err := runCmd(cmd)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			//if err = kube.ApplyManifest(ctx, configFilePath); err != nil {
			//	return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			//}

			meta[InstalledByThisStep] = "true"
			stp.State().Set("deployed", true)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if stp.State().Bool(InstalledByThisStep) == false {
				return automa.StepSkippedReport(stp.Id())
			}

			cmd := fmt.Sprintf("%s/kubectl delete -f %s", core.Paths().SandboxBinDir, configFilePath)
			_, err := runCmd(cmd)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Deploying MetalLB configuration")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to deploy MetalLB configuration")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "MetalLB configuration deployed successfully")
		})
}
