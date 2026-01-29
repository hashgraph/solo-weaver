// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	TeleportNamespace              = "teleport-agent"
	TeleportRelease                = "teleport-agent"
	TeleportChart                  = "teleport/teleport-kube-agent"
	TeleportRepo                   = "https://charts.releases.teleport.dev"
	TeleportDefaultVersion         = "18.6.4"
	SetupTeleportStepId            = "setup-teleport"
	InstallTeleportStepId          = "install-teleport"
	InstallTeleportNodeAgentStepId = "install-teleport-node-agent"
	CreateTeleportNamespaceStepId  = "create-teleport-namespace"
	IsTeleportReadyStepId          = "is-teleport-ready"
)

// buildNodeAgentURL constructs the Teleport node agent install script URL.
// The proxy address must be explicitly specified in config.
func buildNodeAgentURL(proxyAddr, token string) string {
	return fmt.Sprintf("https://%s/scripts/%s/install-node.sh", proxyAddr, token)
}

// getAllowedDomains returns the allowed domains for URL validation.
// For local dev (custom proxy address), the custom address is allowed.
func getAllowedDomains(proxyAddr string) []string {
	// Extract the hostname from the proxy address (remove port if present)
	host := proxyAddr
	if idx := strings.LastIndex(proxyAddr, ":"); idx != -1 {
		// Check if this looks like a port (all digits after colon)
		potentialPort := proxyAddr[idx+1:]
		isPort := true
		for _, c := range potentialPort {
			if c < '0' || c > '9' {
				isPort = false
				break
			}
		}
		if isPort {
			host = proxyAddr[:idx]
		}
	}
	return []string{host}
}

// ConditionalSetupTeleport sets up the Teleport agent if Teleport is enabled.
// This ensures the check and logging happens at execution time, not at workflow build time.
func ConditionalSetupTeleport() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("conditional-setup-teleport").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cfg := config.Get().Teleport
			if !cfg.Enabled {
				logx.As().Info().Msg("Skipping Teleport agent (Teleport disabled in configuration)")
				return automa.StepSkippedReport(stp.Id())
			}

			// Execute the Teleport workflow
			teleportWf, err := SetupTeleport().Build()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			report := teleportWf.Execute(ctx)
			if report.Error != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(report.Error))
			}

			return automa.StepSuccessReport(stp.Id())
		})
}

// SetupTeleport returns a workflow builder that sets up the Teleport Kubernetes agent.
// This provides secure, identity-aware access to the Kubernetes cluster with full audit logging.
// All configuration including RBAC is provided via the Helm values file.
// If NodeAgentToken is configured, it will also install the host-level SSH agent.
func SetupTeleport() *automa.WorkflowBuilder {
	cfg := config.Get().Teleport

	// Build steps list - conditionally include node agent installation
	var steps []automa.Builder

	// If NodeAgentToken is provided, install the host-level SSH agent first
	if cfg.NodeAgentToken != "" {
		steps = append(steps, installTeleportNodeAgent())
	}

	// Always install the Kubernetes agent
	steps = append(steps,
		createTeleportNamespace(),
		installTeleport(),
		isTeleportPodsReady(),
	)

	return automa.NewWorkflowBuilder().WithId(SetupTeleportStepId).Steps(steps...).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Teleport agent")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Teleport agent")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Teleport agent setup successfully")
		})
}

// installTeleportNodeAgent installs the Teleport node agent on the host.
// This provides SSH access to the node via Teleport with full session recording.
// The URL is constructed from the configured proxy address (or default) + the provided join token.
func installTeleportNodeAgent() automa.Builder {
	return automa.NewStepBuilder().WithId(InstallTeleportNodeAgentStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cfg := config.Get()
			teleportCfg := cfg.Teleport
			l := logx.As()

			if teleportCfg.NodeAgentToken == "" {
				l.Info().Msg("Skipping Teleport node agent (no token configured)")
				return automa.StepSkippedReport(stp.Id())
			}

			// Construct the URL from the proxy address and the token
			nodeAgentURL := buildNodeAgentURL(teleportCfg.NodeAgentProxyAddr, teleportCfg.NodeAgentToken)
			l.Info().Str("url", nodeAgentURL).Msg("Installing Teleport node agent")

			// Use the Downloader with allowed domains based on configuration
			allowedDomains := getAllowedDomains(teleportCfg.NodeAgentProxyAddr)
			l.Debug().Strs("allowedDomains", allowedDomains).Msg("URL validation domains")

			// Local profile indicates local development environment
			// Enable insecure TLS for self-signed certs in local dev
			isLocalDev := cfg.IsLocalProfile()
			if isLocalDev {
				l.Debug().Msg("Using insecure TLS for local dev (self-signed certs)")
			}

			downloader := software.NewDownloader(
				software.WithAllowedDomains(allowedDomains),
				software.WithTimeout(2*time.Minute),
				software.WithInsecureTLS(isLocalDev),
			)

			// Download the install script to a temporary file
			scriptPath := path.Join(core.Paths().TempDir, "teleport-install-node.sh")
			if err := downloader.Download(nodeAgentURL, scriptPath); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.InternalError.Wrap(err, "failed to download node agent install script")))
			}

			// Build the command arguments
			// For local dev, use -i flag to ignore existing process/config checks
			// This is needed because the Teleport server container's processes are visible to ps
			// Note: Certificate trust is handled by 'task teleport:start' which adds the cert to system CA store
			args := []string{scriptPath}
			if isLocalDev {
				l.Debug().Msg("Using -i flag for local dev (ignore existing process checks)")
				args = []string{scriptPath, "-i"}
			}

			// Execute the install script
			l.Info().Msg("Executing Teleport node agent install script (requires sudo)")
			cmd := exec.CommandContext(ctx, "bash", args...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr

			if err := cmd.Run(); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.InternalError.Wrap(err, "failed to execute node agent install script")))
			}

			meta := map[string]string{}
			meta[InstalledByThisStep] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing Teleport node agent (SSH access)")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install Teleport node agent")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Teleport node agent installed successfully")
		})
}

func createTeleportNamespace() automa.Builder {
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

func installTeleport() automa.Builder {
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

func isTeleportPodsReady() automa.Builder {
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
