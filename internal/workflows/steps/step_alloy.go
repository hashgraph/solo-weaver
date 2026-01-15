// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"helm.sh/helm/v3/pkg/cli/values"
)

const (
	AlloyNamespace              = "grafana-alloy"
	AlloyRelease                = "grafana-alloy"
	AlloyChart                  = "grafana/alloy"
	AlloyVersion                = "1.4.0"
	AlloyRepo                   = "https://grafana.github.io/helm-charts"
	NodeExporterNamespace       = "node-exporter"
	NodeExporterRelease         = "node-exporter"
	NodeExporterChart           = "oci://registry-1.docker.io/bitnamicharts/node-exporter"
	NodeExporterVersion         = "4.5.19"
	SetupAlloyStepId            = "setup-alloy"
	InstallAlloyStepId          = "install-alloy"
	InstallNodeExporterStepId   = "install-node-exporter"
	AlloyTemplatePath           = "files/alloy/config.alloy"
	DeployAlloyConfigStepId     = "deploy-alloy-config"
	CreateAlloyNamespaceStepId  = "create-alloy-namespace"
	CreateAlloySecretsStepId    = "create-alloy-secrets"
	IsAlloyReadyStepId          = "is-alloy-ready"
	IsNodeExporterReadyStepId   = "is-node-exporter-ready"
	AlloyConfigMapName          = "grafana-alloy-cm"
	AlloySecretsName            = "grafana-alloy-secrets"
	AlloyExternalSecretName     = "grafana-alloy-external-secret"
	AlloyClusterSecretStoreName = "vault-secret-store"
)

// ConditionalSetupObservability sets up the complete observability stack if Alloy is enabled.
// This includes:
// - Prometheus Operator CRDs (for ServiceMonitor/PodMonitor support)
// - Alloy (metrics and logs collection)
// - Node Exporter (system metrics)
// This ensures the check and logging happens at execution time, not at workflow build time.
func ConditionalSetupObservability() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("conditional-setup-observability").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			cfg := config.Get().Alloy
			if !cfg.Enabled {
				logx.As().Info().Msg("Skipping observability stack (Alloy disabled in configuration)")
				return automa.StepSkippedReport(stp.Id())
			}

			// Execute the observability workflow
			observabilityWf, err := SetupObservability().Build()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			report := observabilityWf.Execute(ctx)
			if report.Error != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(report.Error))
			}

			return automa.StepSuccessReport(stp.Id())
		})
}

// SetupObservability returns a workflow builder that sets up the complete observability stack.
// This includes Prometheus Operator CRDs and Grafana Alloy.
func SetupObservability() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("setup-observability").Steps(
		SetupPrometheusOperatorCRDs(), // Install CRDs for ServiceMonitor/PodMonitor
		SetupAlloy(),                  // Install Alloy with Node Exporter
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up observability stack")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup observability stack")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Observability stack setup successfully")
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
			isInstalled, err := hm.IsInstalled(NodeExporterRelease, NodeExporterNamespace)
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
				"serviceMonitor.enabled=false",
				"image.repository=bitnamilegacy/node-exporter",
			}

			_, err = hm.InstallChart(
				ctx,
				NodeExporterRelease,
				NodeExporterChart,
				NodeExporterVersion,
				NodeExporterNamespace,
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

			err = hm.UninstallChart(NodeExporterRelease, NodeExporterNamespace)
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
			err = k.WaitForResources(ctx, kube.KindPod, NodeExporterNamespace, kube.IsPodReady, 5*time.Minute, kube.WaitOptions{NamePrefix: "node-exporter"})
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

			// Create namespace manifest
			namespaceManifestPath := path.Join(core.Paths().TempDir, "alloy-namespace.yaml")
			namespaceManifest := `---
apiVersion: v1
kind: Namespace
metadata:
  name: ` + AlloyNamespace + `
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

			// Create ExternalSecret manifest
			externalSecretPath := path.Join(core.Paths().TempDir, "alloy-external-secret.yaml")

			// Get cluster name from hostname if not provided
			clusterName := cfg.ClusterName
			if clusterName == "" {
				hostname, err := os.Hostname()
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
				clusterName = hostname
			}

			externalSecretManifest := `---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: ` + AlloyExternalSecretName + `
  namespace: ` + AlloyNamespace + `
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: ` + AlloyClusterSecretStoreName + `
    kind: ClusterSecretStore
  target:
    name: ` + AlloySecretsName + `
    creationPolicy: Owner
    template:
      type: Opaque
      engineVersion: v2
      metadata:
        labels:
          app: grafana-alloy
          cluster: ` + clusterName + `
  data:
    - secretKey: PROMETHEUS_PASSWORD
      remoteRef:
        key: "grafana/alloy/` + clusterName + `/prometheus"
        property: password
    - secretKey: LOKI_PASSWORD
      remoteRef:
        key: "grafana/alloy/` + clusterName + `/loki"
        property: password
`

			err = os.WriteFile(externalSecretPath, []byte(externalSecretManifest), 0600)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = k.ApplyManifest(ctx, externalSecretPath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			meta[InstalledByThisStep] = "true"
			stp.State().Set(InstalledByThisStep, true)
			stp.State().Set("externalSecretPath", externalSecretPath)

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

			externalSecretPath := stp.State().String("externalSecretPath")
			if externalSecretPath != "" {
				err = k.DeleteManifest(ctx, externalSecretPath)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating Alloy ExternalSecret")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create Alloy ExternalSecret")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Alloy ExternalSecret created successfully")
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
			isInstalled, err := hm.IsInstalled(AlloyRelease, AlloyNamespace)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if isInstalled {
				meta[AlreadyInstalled] = "true"
				l.Info().Msg("Grafana Alloy is already installed, skipping installation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
			}

			_, err = hm.AddRepo("grafana", AlloyRepo, helm.RepoAddOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Determine if we should use hostNetwork based on URL
			// If the URL contains localhost or 127.0.0.1, enable hostNetwork
			useHostNetwork := strings.Contains(cfg.PrometheusURL, "localhost") ||
				strings.Contains(cfg.PrometheusURL, "127.0.0.1") ||
				strings.Contains(cfg.LokiURL, "localhost") ||
				strings.Contains(cfg.LokiURL, "127.0.0.1")

			// Prepare helm values
			helmValues := []string{
				"crds.create=true",
				"alloy.configMap.create=false",
				"alloy.configMap.name=" + AlloyConfigMapName,
				"alloy.configMap.key=config.alloy",
				"alloy.clustering.enabled=false",
				"alloy.enableReporting=false",
				"alloy.mounts.varlog=false",
				"controller.type=daemonset",
				"serviceMonitor.enabled=true", // Enable ServiceMonitor for Prometheus Operator integration
				// Environment variables from secrets
				"alloy.extraEnv[0].name=PROMETHEUS_PASSWORD",
				"alloy.extraEnv[0].valueFrom.secretKeyRef.name=" + AlloySecretsName,
				"alloy.extraEnv[0].valueFrom.secretKeyRef.key=PROMETHEUS_PASSWORD",
				"alloy.extraEnv[1].name=LOKI_PASSWORD",
				"alloy.extraEnv[1].valueFrom.secretKeyRef.name=" + AlloySecretsName,
				"alloy.extraEnv[1].valueFrom.secretKeyRef.key=LOKI_PASSWORD",
				// Volume mounts for /var/log
				"alloy.mounts.extra[0].name=varlog",
				"alloy.mounts.extra[0].mountPath=/host/var/log",
				"alloy.mounts.extra[0].readOnly=true",
				"controller.volumes.extra[0].name=varlog",
				"controller.volumes.extra[0].hostPath.path=/var/log",
			}

			// Enable hostNetwork if needed for localhost access
			if useHostNetwork {
				helmValues = append(helmValues,
					"controller.hostNetwork=true",
					"controller.dnsPolicy=ClusterFirstWithHostNet",
				)
				l.Info().Msg("Enabling hostNetwork for Alloy to access localhost services")
			}

			chartVersion := AlloyVersion

			_, err = hm.InstallChart(
				ctx,
				AlloyRelease,
				AlloyChart,
				chartVersion,
				AlloyNamespace,
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

			err = hm.UninstallChart(AlloyRelease, AlloyNamespace)
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

			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Get cluster name from hostname if not provided
			clusterName := cfg.ClusterName
			if clusterName == "" {
				hostname, err := os.Hostname()
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
				clusterName = hostname
			}

			// Render the Alloy configuration template
			tmplData := templates.AlloyData{
				ClusterName:        clusterName,
				PrometheusURL:      cfg.PrometheusURL,
				PrometheusUsername: cfg.PrometheusUsername,
				LokiURL:            cfg.LokiURL,
				LokiUsername:       cfg.LokiUsername,
				MonitorBlockNode:   cfg.MonitorBlockNode,
			}

			renderedConfig, err := templates.Render(AlloyTemplatePath, tmplData)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Create ConfigMap manifest with the rendered configuration
			configMapManifest := `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: ` + AlloyConfigMapName + `
  namespace: ` + AlloyNamespace + `
data:
  config.alloy: |
` + indentLines(renderedConfig, "    ")

			// Write manifest to temp file for kubectl apply
			configMapManifestPath := path.Join(core.Paths().TempDir, "alloy-configmap.yaml")
			err = os.WriteFile(configMapManifestPath, []byte(configMapManifest), 0644)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// Apply the ConfigMap
			err = k.ApplyManifest(ctx, configMapManifestPath)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta[InstalledByThisStep] = "true"
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

// indentLines adds the specified indentation to each line of the text
func indentLines(text, indent string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
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
			err = k.WaitForResources(ctx, kube.KindPod, AlloyNamespace, kube.IsPodReady, 5*time.Minute, kube.WaitOptions{NamePrefix: "grafana-alloy"})
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
