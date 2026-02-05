// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	TeleportNamespace             = "teleport-agent"
	TeleportRelease               = "teleport-agent"
	TeleportChart                 = "teleport/teleport-kube-agent"
	TeleportRepo                  = "https://charts.releases.teleport.dev"
	TeleportDefaultVersion        = deps.TELEPORT_VERSION
	SetupTeleportStepId           = "setup-teleport"
	InstallTeleportStepId         = "install-teleport"
	CreateTeleportNamespaceStepId = "create-teleport-namespace"
	IsTeleportReadyStepId         = "is-teleport-ready"
)

// SetupTeleportNodeAgent returns a workflow builder that sets up the Teleport node agent.
// This provides SSH access to the node via Teleport with full session recording.
// Used by 'solo-provisioner teleport node install' command.
func SetupTeleportNodeAgent() *automa.WorkflowBuilder {
	cfg := config.Get().Teleport

	return automa.NewWorkflowBuilder().WithId("setup-teleport-node-agent").
		Steps(
			installTeleportNodeAgent(newTeleportInstallerProvider(cfg)),
			configureTeleportNodeAgent(newTeleportInstallerProvider(cfg)),
			SetupSystemdService(software.TeleportServiceName),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Teleport node agent")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Teleport node agent")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Teleport node agent setup successfully")
		})
}

// SetupTeleportClusterAgent returns a workflow builder that sets up the Teleport Kubernetes agent.
// This provides secure, identity-aware access to the Kubernetes cluster with full audit logging.
// All configuration including RBAC is provided via the Helm values file.
// Used by 'solol-provisioner teleport cluster install' command.
func SetupTeleportClusterAgent() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId(SetupTeleportStepId).
		Steps(
			CreateTeleportNamespace(),
			InstallTeleportKubeAgent(),
			IsTeleportPodsReady(),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Teleport cluster agent")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Teleport cluster agent")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Teleport cluster agent setup successfully")
		})
}

// teleportInstallerProvider is a function type that creates a teleport installer with config
type teleportInstallerProvider func(opts ...software.InstallerOption) (software.Software, error)

// newTeleportInstallerProvider creates a provider function that includes the teleport configuration
func newTeleportInstallerProvider(cfg config.TeleportConfig) teleportInstallerProvider {
	return func(opts ...software.InstallerOption) (software.Software, error) {
		configOpts := &software.TeleportNodeAgentConfigureOptions{
			ProxyAddr: cfg.NodeAgentProxyAddr,
			JoinToken: cfg.NodeAgentToken,
		}

		return software.NewTeleportNodeAgentInstallerWithConfig(configOpts, opts...)
	}
}

// installTeleportNodeAgent installs the Teleport binaries (following kubelet pattern)
func installTeleportNodeAgent(provider teleportInstallerProvider) automa.Builder {
	return automa.NewStepBuilder().WithId("install-teleport-node-agent").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing Teleport node agent binaries")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install Teleport node agent binaries")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Teleport node agent binaries installed successfully")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			// Prepare metadata for reporting
			meta := map[string]string{}

			installed, err := installer.IsInstalled()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			if installed {
				meta[AlreadyInstalled] = "true"
				return automa.SkippedReport(stp, automa.WithDetail("teleport is already installed"), automa.WithMetadata(meta))
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

			err = installer.Install()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[InstalledByThisStep] = "true"
			stp.State().Set(InstalledByThisStep, true)

			err = installer.Cleanup()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[CleanedUpByThisStep] = "true"

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			installedByThisStep := stp.State().Bool(InstalledByThisStep)
			if !installedByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("teleport was not installed by this step, skipping rollback"))
			}

			installer, err := provider()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			err = installer.Uninstall()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}

// configureTeleportNodeAgent configures the Teleport node agent (following kubelet pattern)
// This runs "teleport configure" to generate the config file and creates the systemd service
func configureTeleportNodeAgent(provider teleportInstallerProvider) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-teleport-node-agent").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Configuring Teleport node agent")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to configure Teleport node agent")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Teleport node agent configured successfully")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			// Prepare metadata for reporting
			meta := map[string]string{}

			configured, err := installer.IsConfigured()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			if configured {
				meta[AlreadyConfigured] = "true"
				return automa.SkippedReport(stp, automa.WithDetail("teleport is already configured"), automa.WithMetadata(meta))
			}

			err = installer.Configure()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[ConfiguredByThisStep] = "true"
			stp.State().Set(ConfiguredByThisStep, true)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			configuredByThisStep := stp.State().Bool(ConfiguredByThisStep)
			if !configuredByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("teleport was not configured by this step, skipping rollback"))
			}

			installer, err := provider()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			err = installer.RemoveConfiguration()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}

func CreateTeleportNamespace() automa.Builder {
	return automa.NewStepBuilder().WithId(CreateTeleportNamespaceStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Create namespace manifest
			namespaceManifestPath := path.Join(core.Paths().TempDir, "teleport-namespace.yaml")
			namespaceManifest := `---
apiVersion: v1
kind: Namespace
metadata:
  name: ` + TeleportNamespace + `
`

			err = os.WriteFile(namespaceManifestPath, []byte(namespaceManifest), 0644)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = k.ApplyManifest(ctx, namespaceManifestPath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			meta[InstalledByThisStep] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating Teleport namespace")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create Teleport namespace")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Teleport namespace created successfully")
		})
}

func InstallTeleportKubeAgent() automa.Builder {
	return automa.NewStepBuilder().WithId(InstallTeleportStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cfg := config.Get().Teleport

			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(TeleportRelease, TeleportNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("Teleport agent is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			_, err = hm.AddRepo("teleport", TeleportRepo, helm.RepoAddOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Determine chart version
			chartVersion := cfg.Version
			if chartVersion == "" {
				chartVersion = TeleportDefaultVersion
			}

			l.Info().Str("path", cfg.ValuesFile).Msg("Using Teleport values file")

			_, err = hm.InstallChart(
				ctx,
				TeleportRelease,
				TeleportChart,
				chartVersion,
				TeleportNamespace,
				helm.InstallChartOptions{
					ValueOpts: &values.Options{
						ValueFiles: []string{cfg.ValuesFile},
					},
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
			stp.State().Set(InstalledByThisStep, true)

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

			err = hm.UninstallChart(TeleportRelease, TeleportNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing Teleport agent")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install Teleport agent")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Teleport agent installed successfully")
		})
}

func IsTeleportPodsReady() automa.Builder {
	return automa.NewStepBuilder().WithId(IsTeleportReadyStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {

			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			// wait for teleport pods to be ready
			err = k.WaitForResources(ctx, kube.KindPod, TeleportNamespace, kube.IsPodReady, 5*time.Minute, kube.WaitOptions{NamePrefix: "teleport-agent"})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[IsReady] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Verifying Teleport agent readiness")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Teleport agent is not ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Teleport agent is ready")
		})
}
