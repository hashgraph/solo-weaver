// SPDX-License-Identifier: Apache-2.0

package deps

const (
	// Block Node
	BLOCK_NODE_NAMESPACE         = "block-node"
	BLOCK_NODE_RELEASE           = "block-node"
	BLOCK_NODE_CHART             = "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
	BLOCK_NODE_VERSION           = "0.29.0"
	BLOCK_NODE_STORAGE_BASE_PATH = "/mnt/fast-storage"

	// Teleport
	TELEPORT_NAMESPACE = "teleport-agent"
	TELEPORT_RELEASE   = "teleport-agent"
	TELEPORT_CHART     = "teleport/teleport-kube-agent"
	TELEPORT_REPO      = "https://charts.releases.teleport.dev"
	TELEPORT_VERSION   = "18.6.4"

	// Grafana Alloy
	ALLOY_NAMESPACE = "grafana-alloy"
	ALLOY_RELEASE   = "grafana-alloy"
	ALLOY_CHART     = "grafana/alloy"
	ALLOY_VERSION   = "1.4.0"
	ALLOY_REPO      = "https://grafana.github.io/helm-charts"

	// Node Exporter
	NODE_EXPORTER_NAMESPACE = "node-exporter"
	NODE_EXPORTER_RELEASE   = "node-exporter"
	NODE_EXPORTER_CHART     = "oci://registry-1.docker.io/bitnamicharts/node-exporter"
	NODE_EXPORTER_VERSION   = "4.5.19"

	// MetalLB
	METALLB_NAMESPACE = "metallb-system"
	METALLB_RELEASE   = "metallb"
	METALLB_CHART     = "metallb/metallb"
	METALLB_VERSION   = "0.15.2"
	METALLB_REPO      = "https://metallb.github.io/metallb"

	// Metrics Server
	METRICS_SERVER_NAMESPACE = "kube-system"
	METRICS_SERVER_RELEASE   = "metrics-server"
	METRICS_SERVER_CHART     = "metrics-server/metrics-server"
	METRICS_SERVER_VERSION   = "3.13.0"
	METRICS_SERVER_REPO      = "https://kubernetes-sigs.github.io/metrics-server"

	// Prometheus Operator CRDs
	PROMETHEUS_OPERATOR_CRDS_NAMESPACE = "grafana-alloy"
	PROMETHEUS_OPERATOR_CRDS_RELEASE   = "prometheus-operator-crds"
	PROMETHEUS_OPERATOR_CRDS_CHART     = "oci://ghcr.io/prometheus-community/charts/prometheus-operator-crds"
	PROMETHEUS_OPERATOR_CRDS_VERSION   = "24.0.1"

	// External Secrets
	EXTERNAL_SECRETS_NAMESPACE = "external-secrets"
	EXTERNAL_SECRETS_RELEASE   = "external-secrets"
	EXTERNAL_SECRETS_CHART     = "external-secrets/external-secrets"
	EXTERNAL_SECRETS_VERSION   = "0.20.2"
	EXTERNAL_SECRETS_REPO      = "https://charts.external-secrets.io"
)
