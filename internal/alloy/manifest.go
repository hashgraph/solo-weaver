// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"github.com/hashgraph/solo-weaver/internal/config"
)

// ConfigMapManifest generates the Alloy ConfigMap manifest.
func ConfigMapManifest(renderedConfig string) string {
	return `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: ` + ConfigMapName + `
  namespace: ` + Namespace + `
data:
  config.alloy: |
` + IndentLines(renderedConfig, "    ")
}

// ExternalSecretManifest generates the Alloy ExternalSecret manifest.
func ExternalSecretManifest(cfg config.AlloyConfig, clusterName string) string {
	secretDataEntries := BuildExternalSecretDataEntries(cfg, clusterName)

	return `---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: ` + ExternalSecretName + `
  namespace: ` + Namespace + `
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: ` + ClusterSecretStoreName + `
    kind: ClusterSecretStore
  target:
    name: ` + SecretsName + `
    creationPolicy: Owner
    template:
      type: Opaque
      engineVersion: v2
      metadata:
        labels:
          app: grafana-alloy
          cluster: ` + clusterName + `
  data:
` + secretDataEntries
}

// BaseHelmValues returns the base Helm values for Alloy installation.
func BaseHelmValues() []string {
	return []string{
		"crds.create=true",
		"alloy.configMap.create=false",
		"alloy.configMap.name=" + ConfigMapName,
		"alloy.configMap.key=config.alloy",
		"alloy.clustering.enabled=false",
		"alloy.enableReporting=false",
		"alloy.mounts.varlog=false",
		"controller.type=daemonset",
		"serviceMonitor.enabled=true",
		// Volume mounts for /var/log
		"alloy.mounts.extra[0].name=varlog",
		"alloy.mounts.extra[0].mountPath=/host/var/log",
		"alloy.mounts.extra[0].readOnly=true",
		"controller.volumes.extra[0].name=varlog",
		"controller.volumes.extra[0].hostPath.path=/var/log",
	}
}

// HostNetworkHelmValues returns Helm values for enabling host network access.
func HostNetworkHelmValues() []string {
	return []string{
		"controller.hostNetwork=true",
		"controller.dnsPolicy=ClusterFirstWithHostNet",
	}
}
