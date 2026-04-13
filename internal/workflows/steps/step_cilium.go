// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/models"

	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

func SetupCilium(mr software.MachineRuntime) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("setup-cilium-cli").Steps(
		installCilium(software.NewCiliumInstaller, mr),
		configureCilium(software.NewCiliumInstaller, mr),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Cilium")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Cilium")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cilium setup successfully")
		})
}

func installCilium(provider func(opts ...software.InstallerOption) (software.Software, error), mr software.MachineRuntime) automa.Builder {
	return automa.NewStepBuilder().WithId("install-cilium-cli").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "installing Cilium CLI...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider(software.WithMachineRuntime(mr))
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			logx.As().Info().Str("step_id", stp.Id()).Str("software", installer.GetSoftwareName()).Str("version", installer.Version()).Msgf("%s version: %s", installer.GetSoftwareName(), installer.Version())

			// Prepare metadata for reporting
			meta := map[string]string{}

			installed, err := installer.IsInstalled()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			if installed {
				meta[AlreadyInstalled] = "true"
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CLI is already installed"), automa.WithMetadata(meta))
			}

			err = installer.Download()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[DownloadedByThisStep] = "true"

			err = installer.Extract()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[ExtractedByThisStep] = "true"

			err = installer.Install()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[InstalledByThisStep] = "true"
			stp.State().Local().Set(InstalledByThisStep, true)

			err = installer.Cleanup()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[CleanedUpByThisStep] = "true"

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			var installedByThisStep bool
			if v, ok := stp.State().Local().Bool(InstalledByThisStep); ok {
				installedByThisStep = v
			}

			if !installedByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CLI was not installed by this step, skipping rollback"))
			}

			installer, err := provider(software.WithMachineRuntime(mr))
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			err = installer.Uninstall()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}

func configureCilium(provider func(opts ...software.InstallerOption) (software.Software, error), mr software.MachineRuntime) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-cilium-cli").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "configuring Cilium CLI...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider(software.WithMachineRuntime(mr))
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			// Prepare metadata for reporting
			meta := map[string]string{}

			configured, err := installer.IsConfigured()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			if configured {
				meta[AlreadyConfigured] = "true"
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CLI is already configured"), automa.WithMetadata(meta))
			}

			err = installer.Configure()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Local().Set(ConfiguredByThisStep, true)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			var configuredByThisStep bool
			if v, ok := stp.State().Local().Bool(ConfiguredByThisStep); ok {
				configuredByThisStep = v
			}

			if !configuredByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CLI was not configured by this step, skipping rollback"))
			}

			installer, err := provider(software.WithMachineRuntime(mr))
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			err = installer.RemoveConfiguration()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}

func StartCilium() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("start-cilium").Steps(
		installCiliumCNI("1.18.1"), // we cannot write in pure Go because we need to run cilium binary

	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Starting Cilium")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to start Cilium")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cilium started successfully")
		})
}

// TODO to be replaced with helm invocation
func installCiliumCNI(version string) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("install-cilium-cni").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "installing Cilium CNI...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			logx.As().Info().Str("step_id", stp.Id()).Str("software", "cilium-cni").Str("version", version).Msgf("cilium-cni version: %s", version)

			// Prepare metadata for reporting
			meta := map[string]string{}

			// Check if Cilium CNI is already installed/running using cilium status
			statusCheck := []string{
				fmt.Sprintf("%s/cilium status", models.Paths().SandboxBinDir),
			}
			output, err := automa_steps.RunBashScript(statusCheck, "")
			if err == nil && output != "" {
				// If cilium status succeeds, Cilium is already installed and running
				meta[AlreadyInstalled] = "true"
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CNI is already installed and running"), automa.WithMetadata(meta))
			}

			// Install Cilium CNI
			installScript := []string{
				fmt.Sprintf("/usr/bin/sudo %s/cilium install --wait --version \"%s\" --values %s/etc/weaver/cilium-config.yaml",
					models.Paths().SandboxBinDir, version, models.Paths().SandboxDir),
			}
			_, err = automa_steps.RunBashScript(installScript, "")
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}

			meta[InstalledByThisStep] = "true"
			stp.State().Local().Set(InstalledByThisStep, true)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			var installedByThisStep bool
			if v, ok := stp.State().Local().Bool(InstalledByThisStep); ok {
				installedByThisStep = v
			}
			if !installedByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CNI was not installed by this step, skipping rollback"))
			}

			// Uninstall Cilium CNI
			scripts := []string{
				fmt.Sprintf("/usr/bin/sudo %s/cilium uninstall", models.Paths().SandboxBinDir),
			}
			_, err := automa_steps.RunBashScript(scripts, "")
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}
