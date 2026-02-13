// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/alloy"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	SetupAlloyStepId           = "setup-alloy"
	InstallAlloyStepId         = "install-alloy"
	InstallNodeExporterStepId  = "install-node-exporter"
	DeployAlloyConfigStepId    = "deploy-alloy-config"
	CreateAlloyNamespaceStepId = "create-alloy-namespace"
	CreateAlloySecretsStepId   = "create-alloy-secrets"
	IsAlloyReadyStepId         = "is-alloy-ready"
	IsNodeExporterReadyStepId  = "is-node-exporter-ready"
)

// SetupAlloyStack returns a workflow builder that sets up the complete Alloy observability stack.
// This includes Prometheus Operator CRDs and Grafana Alloy.
func SetupAlloyStack() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("setup-alloy-stack").Steps(
		SetupExternalSecrets(),        // External Secrets Operator (general-purpose secret management for the cluster)
		SetupPrometheusOperatorCRDs(), // Install CRDs for ServiceMonitor/PodMonitor
		SetupAlloy(),                  // Install Alloy with Node Exporter
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Alloy observability stack")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Alloy observability stack")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Alloy observability stack setup successfully")
		})
}

// TeardownAlloyStack returns a workflow builder that tears down the complete Alloy observability stack.
// This removes Grafana Alloy, Node Exporter, and Prometheus Operator CRDs.
func TeardownAlloyStack() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("teardown-alloy-stack").Steps(
		uninstallAlloy(),
		uninstallNodeExporter(),
		TeardownPrometheusOperatorCRDs(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Tearing down Alloy observability stack")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to teardown Alloy observability stack")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Alloy observability stack torn down successfully")
		})
}

// SetupAlloy returns a workflow builder that sets up Grafana Alloy for observability.
func SetupAlloy() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId(SetupAlloyStepId).Steps(
		createAlloyNamespace(),
		installNodeExporter(),
		isNodeExporterPodsReady(),
		createAlloyExternalSecret(),
		deployAlloyConfig(),
		installAlloy(),
		isAlloyPodsReady(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Grafana Alloy")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Grafana Alloy")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Grafana Alloy setup successfully")
		})
}

func installNodeExporter() automa.Builder {
	return automa.NewStepBuilder().WithId(InstallNodeExporterStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(alloy.NodeExporterRelease, alloy.NodeExporterNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("Node Exporter is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			helmValues := []string{
				"resourceType=daemonset",
				"resourcesPreset=small",
				"rbac.pspEnabled=false",
				"serviceMonitor.enabled=true",
				"serviceMonitor.interval=15s",
				"serviceMonitor.scrapeTimeout=5s",
				"serviceMonitor.jobLabel=node",
				"serviceMonitor.attachMetadata.node=true",
				"extraArgs.collector\\.systemd=",
				"extraArgs.collector\\.processes=",
				"image.repository=bitnamilegacy/node-exporter",
			}

			_, err = hm.InstallChart(
				ctx,
				alloy.NodeExporterRelease,
				alloy.NodeExporterChart,
				alloy.NodeExporterVersion,
				alloy.NodeExporterNamespace,
				helm.InstallChartOptions{
					ValueOpts: &values.Options{
						Values: helmValues,
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

			err = hm.UninstallChart(alloy.NodeExporterRelease, alloy.NodeExporterNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing Node Exporter")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install Node Exporter")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Node Exporter installed successfully")
		})
}

func isNodeExporterPodsReady() automa.Builder {
	return automa.NewStepBuilder().WithId(IsNodeExporterReadyStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			// wait for node-exporter pods to be ready
			err = k.WaitForResources(ctx, kube.KindPod, alloy.NodeExporterNamespace, kube.IsPodReady, 5*time.Minute, kube.WaitOptions{NamePrefix: "node-exporter"})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[IsReady] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Verifying Node Exporter readiness")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Node Exporter is not ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Node Exporter is ready")
		})
}

func createAlloyNamespace() automa.Builder {
	return automa.NewStepBuilder().WithId(CreateAlloyNamespaceStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Create namespace manifest using template
			namespaceManifestPath := path.Join(core.Paths().TempDir, "alloy-namespace.yaml")
			namespaceManifest, err := alloy.NamespaceManifest()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = os.WriteFile(namespaceManifestPath, []byte(namespaceManifest), core.DefaultFilePerm)
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
			notify.As().StepStart(ctx, stp, "Creating Alloy namespace")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create Alloy namespace")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Alloy namespace created successfully")
		})
}

func createAlloyExternalSecret() automa.Builder {
	return automa.NewStepBuilder().WithId(CreateAlloySecretsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cfg := config.Get().Alloy

			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Check if remotes are configured
			hasRemotes := len(cfg.PrometheusRemotes) > 0 || len(cfg.LokiRemotes) > 0 ||
				cfg.PrometheusURL != "" || cfg.LokiURL != ""

			meta := map[string]string{}
			var manifestPath string
			var manifest string

			if hasRemotes {
				// Create ExternalSecret to fetch passwords from Vault
				manifestPath = path.Join(core.Paths().TempDir, "alloy-external-secret.yaml")

				// Build config to get cluster name
				cb, err := alloy.NewConfigBuilder(cfg)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
				clusterName := cb.ClusterName()

				// Generate the ExternalSecret manifest using the alloy package
				manifest, err = alloy.ExternalSecretManifest(cfg, clusterName)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
			} else {
				// Create an empty secret so the pod doesn't fail looking for it
				manifestPath = path.Join(core.Paths().TempDir, "alloy-empty-secret.yaml")
				manifest, err = alloy.EmptySecretManifest()
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
			}

			err = os.WriteFile(manifestPath, []byte(manifest), 0600)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = k.ApplyManifest(ctx, manifestPath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[InstalledByThisStep] = "true"
			stp.State().Set(InstalledByThisStep, true)
			stp.State().Set("secretManifestPath", manifestPath)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if stp.State().Bool(InstalledByThisStep) == false {
				return automa.StepSkippedReport(stp.Id())
			}

			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			manifestPath := stp.State().String("secretManifestPath")
			if manifestPath != "" {
				err = k.DeleteManifest(ctx, manifestPath)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating Alloy secrets")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create Alloy secrets")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Alloy secrets created successfully")
		})
}

func installAlloy() automa.Builder {
	return automa.NewStepBuilder().WithId(InstallAlloyStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cfg := config.Get().Alloy

			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}

			// Check if already installed to provide better logging and track for rollback
			isInstalled, err := hm.IsInstalled(alloy.Release, alloy.Namespace)
			if err != nil {
				l.Warn().Err(err).Msg("Failed to check if Grafana Alloy is installed, proceeding with install/upgrade")
			}

			// Track whether this is a fresh install (for rollback purposes)
			isFreshInstall := !isInstalled

			if isInstalled {
				l.Info().Msg("Grafana Alloy is already installed, upgrading configuration")
			} else {
				l.Info().Msg("Installing Grafana Alloy")
			}

			_, err = hm.AddRepo("grafana", alloy.Repo, helm.RepoAddOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Build config to check if host network is needed
			cb, err := alloy.NewConfigBuilder(cfg)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			useHostNetwork := cb.ShouldUseHostNetwork()

			// Prepare helm values
			helmValues := alloy.BaseHelmValues()

			// Add environment variables for all configured remotes
			envVars := alloy.BuildHelmEnvVars(cfg)
			helmValues = append(helmValues, envVars...)

			// Enable hostNetwork if needed for localhost access
			if useHostNetwork {
				helmValues = append(helmValues, alloy.HostNetworkHelmValues()...)
				l.Info().Msg("Enabling hostNetwork for Alloy to access localhost services")
			}

			// Use DeployChart for idempotent install/upgrade
			// This will install if not present, or upgrade if already installed
			_, err = hm.DeployChart(
				ctx,
				alloy.Release,
				alloy.Chart,
				alloy.Version,
				alloy.Namespace,
				helm.DeployChartOptions{
					ValueOpts: &values.Options{
						Values: helmValues,
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

			// Only set InstalledByThisStep if this was a fresh install
			// This prevents rollback from uninstalling a pre-existing release
			if isFreshInstall {
				meta[InstalledByThisStep] = "true"
				stp.State().Set(InstalledByThisStep, true)
			} else {
				meta["upgraded"] = "true"
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

			err = hm.UninstallChart(alloy.Release, alloy.Namespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Installing Grafana Alloy")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to install Grafana Alloy")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Grafana Alloy installed successfully")
		})
}

func deployAlloyConfig() automa.Builder {
	return automa.NewStepBuilder().WithId(DeployAlloyConfigStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cfg := config.Get().Alloy
			meta := map[string]string{}

			l := logx.As()
			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Build config using the alloy package
			cb, err := alloy.NewConfigBuilder(cfg)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Render Alloy configuration modules
			modules, err := alloy.RenderModularConfigs(cb)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			moduleNames := alloy.GetModuleNames(modules)

			// Log which modules are being installed
			l.Info().
				Strs("modules", moduleNames).
				Str("clusterName", cb.ClusterName()).
				Bool("monitorBlockNode", cb.MonitorBlockNode()).
				Msg("Alloy configuration modules")

			// Create ConfigMap manifest with multiple .alloy files
			configMapManifest, err := alloy.ConfigMapManifest(modules)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Write manifest to temp file for kubectl apply
			configMapManifestPath := path.Join(core.Paths().TempDir, "alloy-configmap.yaml")
			err = os.WriteFile(configMapManifestPath, []byte(configMapManifest), core.DefaultFilePerm)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Apply the ConfigMap
			err = k.ApplyManifest(ctx, configMapManifestPath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// If monitoring block-node, deploy the ServiceMonitor and PodLogs for discovery
			if cb.MonitorBlockNode() {
				// Discover block node namespace from Helm
				blockNodeNamespace, err := state.GetBlockNodeNamespace()
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(fmt.Errorf("failed to discover block node namespace: %w", err)))
				}
				if blockNodeNamespace == "" {
					l.Warn().Msg("Block node monitoring enabled but block node not found; skipping ServiceMonitor and PodLogs deployment")
				} else {
					// Deploy ServiceMonitor for metrics discovery
					serviceMonitorManifest, err := alloy.BlockNodeServiceMonitorManifest(blockNodeNamespace)
					if err != nil {
						return automa.StepFailureReport(stp.Id(), automa.WithError(err))
					}
					serviceMonitorPath := path.Join(core.Paths().TempDir, "block-node-servicemonitor.yaml")
					err = os.WriteFile(serviceMonitorPath, []byte(serviceMonitorManifest), core.DefaultFilePerm)
					if err != nil {
						return automa.StepFailureReport(stp.Id(), automa.WithError(err))
					}

					err = k.ApplyManifest(ctx, serviceMonitorPath)
					if err != nil {
						return automa.StepFailureReport(stp.Id(), automa.WithError(err))
					}
					stp.State().Set("serviceMonitorPath", serviceMonitorPath)

					// Deploy PodLogs for logs discovery
					podLogsManifest, err := alloy.BlockNodePodLogsManifest(blockNodeNamespace)
					if err != nil {
						return automa.StepFailureReport(stp.Id(), automa.WithError(err))
					}
					podLogsPath := path.Join(core.Paths().TempDir, "block-node-podlogs.yaml")
					err = os.WriteFile(podLogsPath, []byte(podLogsManifest), core.DefaultFilePerm)
					if err != nil {
						return automa.StepFailureReport(stp.Id(), automa.WithError(err))
					}

					err = k.ApplyManifest(ctx, podLogsPath)
					if err != nil {
						return automa.StepFailureReport(stp.Id(), automa.WithError(err))
					}
					stp.State().Set("podLogsPath", podLogsPath)

					l.Info().Str("namespace", blockNodeNamespace).Msg("Block Node ServiceMonitor and PodLogs deployed for metrics/logs discovery")
				}
			}

			meta[InstalledByThisStep] = "true"
			meta["modules"] = strings.Join(moduleNames, ",")
			stp.State().Set(InstalledByThisStep, true)
			stp.State().Set("configMapManifestPath", configMapManifestPath)

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if stp.State().Bool(InstalledByThisStep) == false {
				return automa.StepSkippedReport(stp.Id())
			}

			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			configMapManifestPath := stp.State().String("configMapManifestPath")
			if configMapManifestPath != "" {
				err = k.DeleteManifest(ctx, configMapManifestPath)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
			}

			// Clean up ServiceMonitor if it was deployed
			serviceMonitorPath := stp.State().String("serviceMonitorPath")
			if serviceMonitorPath != "" {
				_ = k.DeleteManifest(ctx, serviceMonitorPath) // Ignore error - may not exist
			}

			// Clean up PodLogs if it was deployed
			podLogsPath := stp.State().String("podLogsPath")
			if podLogsPath != "" {
				_ = k.DeleteManifest(ctx, podLogsPath) // Ignore error - may not exist
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Deploying Alloy configuration")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to deploy Alloy configuration")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Alloy configuration deployed successfully")
		})
}

func isAlloyPodsReady() automa.Builder {
	return automa.NewStepBuilder().WithId(IsAlloyReadyStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {

			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			// wait for alloy pods to be ready
			err = k.WaitForResources(ctx, kube.KindPod, alloy.Namespace, kube.IsPodReady, 5*time.Minute, kube.WaitOptions{NamePrefix: "grafana-alloy"})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[IsReady] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Verifying Alloy readiness")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Alloy is not ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Alloy is ready")
		})
}

// uninstallAlloy removes the Grafana Alloy installation
func uninstallAlloy() automa.Builder {
	return automa.NewStepBuilder().WithId("uninstall-alloy").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(alloy.Release, alloy.Namespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if !isInstalled {
				l.Info().Msg("Grafana Alloy is not installed, skipping uninstallation")
				return automa.StepSkippedReport(stp.Id())
			}

			err = hm.UninstallChart(alloy.Release, alloy.Namespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta["uninstalled"] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Uninstalling Grafana Alloy")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to uninstall Grafana Alloy")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Grafana Alloy uninstalled successfully")
		})
}

// uninstallNodeExporter removes the Node Exporter installation
func uninstallNodeExporter() automa.Builder {
	return automa.NewStepBuilder().WithId("uninstall-node-exporter").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			hm, err := helm.NewManager(helm.WithLogger(*l))
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			isInstalled, err := hm.IsInstalled(alloy.NodeExporterRelease, alloy.NodeExporterNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if !isInstalled {
				l.Info().Msg("Node Exporter is not installed, skipping uninstallation")
				return automa.StepSkippedReport(stp.Id())
			}

			err = hm.UninstallChart(alloy.NodeExporterRelease, alloy.NodeExporterNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta["uninstalled"] = "true"
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Uninstalling Node Exporter")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to uninstall Node Exporter")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Node Exporter uninstalled successfully")
		})
}
