// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/kube"
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
		guardBandwidthManagerDisabled(),
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

// Cilium agent config surfaces the Bandwidth Manager guard inspects. The
// cilium-config ConfigMap is created by `cilium install` and read by the agent.
const (
	ciliumConfigMapNamespace = "kube-system"
	ciliumConfigMapName      = "cilium-config"
	// bandwidthManagerConfigKey is the cilium-config data key set by the Cilium
	// agent flag --enable-bandwidth-manager. "true" when enabled; "false" or
	// absent when disabled.
	bandwidthManagerConfigKey = "enable-bandwidth-manager"
)

// bandwidthManagerEnabled reports whether a cilium-config enable-bandwidth-manager
// value means the Bandwidth Manager is on. An empty value (key absent) is disabled.
func bandwidthManagerEnabled(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

// ciliumBandwidthManagerState reads the cilium-config ConfigMap via the Kubernetes
// API (pure Go, no shell) and reports whether the Bandwidth Manager is enabled
// along with the raw enable-bandwidth-manager value. Mirrors the cilium-config
// read in migration_cilium_acceleration.go. It errors if cilium-config is absent,
// since the guard cannot otherwise verify the precondition.
func ciliumBandwidthManagerState(ctx context.Context, k *kube.Client) (enabled bool, value string, err error) {
	exists, err := k.ResourceExists(ctx, "v1", "ConfigMap", ciliumConfigMapNamespace, ciliumConfigMapName)
	if err != nil {
		return false, "", errorx.ExternalError.Wrap(err, "failed to check for the %q ConfigMap in namespace %q", ciliumConfigMapName, ciliumConfigMapNamespace).
			WithProperty(models.ErrPropertyResolution, []string{
				"Ensure the cluster API is reachable (kubeconfig valid, API server up):",
				"  kubectl -n kube-system get configmap cilium-config",
			})
	}
	if !exists {
		return false, "", errorx.IllegalState.New("Cilium ConfigMap %q not found in namespace %q", ciliumConfigMapName, ciliumConfigMapNamespace).
			WithProperty(models.ErrPropertyResolution, []string{
				"Confirm Cilium is installed and its ConfigMap exists:",
				"  kubectl -n kube-system get configmap cilium-config -o yaml",
			})
	}

	value, err = k.GetResourceNestedString(ctx, "v1", "ConfigMap", ciliumConfigMapNamespace, ciliumConfigMapName, "data", bandwidthManagerConfigKey)
	if err != nil {
		return false, "", errorx.ExternalError.Wrap(err, "failed to read %q from the %q ConfigMap", bandwidthManagerConfigKey, ciliumConfigMapName).
			WithProperty(models.ErrPropertyResolution, []string{
				"Ensure the cluster API is reachable (kubeconfig valid, API server up):",
				"  kubectl -n kube-system get configmap cilium-config -o yaml",
			})
	}
	return bandwidthManagerEnabled(value), value, nil
}

// guardBandwidthManagerDisabled fails fast if Cilium's Bandwidth Manager is
// enabled. Bandwidth Manager is the only Cilium BPF writer of skb->priority, so
// the BN traffic shaper's egress-priority survival guarantee is void whenever it
// is on (traffic-shaper v4 design §10 risk 18). It reads cilium-config through the
// Kubernetes API (pure Go, no shell) and is read-only, so there is no rollback.
func guardBandwidthManagerDisabled() automa.Builder {
	return automa.NewStepBuilder().WithId("guard-bandwidth-manager-disabled").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "verifying Cilium Bandwidth Manager is disabled...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}
			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			enabled, value, err := ciliumBandwidthManagerState(ctx, k)
			if err != nil {
				// ciliumBandwidthManagerState types its errors: ExternalError for
				// kube-API failures, IllegalState for a missing ConfigMap. Forward
				// as-is so the classification (and resolution hints) are preserved.
				return automa.FailureReport(stp,
					automa.WithError(err),
					automa.WithMetadata(meta))
			}

			meta[BandwidthManagerStatus] = value

			if enabled {
				return automa.FailureReport(stp,
					automa.WithError(errorx.IllegalState.New(
						"Cilium Bandwidth Manager is enabled (%s=%q) — it is the only Cilium BPF writer of skb->priority and would void the BN egress-priority guarantee; it must be Disabled", bandwidthManagerConfigKey, value).
						WithProperty(models.ErrPropertyResolution, []string{
							"Disable the Cilium Bandwidth Manager, then re-run the install:",
							"  set 'bandwidthManager.enabled: false' in the Cilium Helm values and run 'cilium upgrade'",
							"Verify with: kubectl -n kube-system get configmap cilium-config -o jsonpath='{.data.enable-bandwidth-manager}'",
						})),
					automa.WithMetadata(meta))
			}

			return automa.SuccessReport(stp,
				automa.WithDetail("Cilium Bandwidth Manager is disabled"),
				automa.WithMetadata(meta))
		})
}
