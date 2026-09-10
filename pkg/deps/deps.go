// SPDX-License-Identifier: Apache-2.0

// Package deps holds compile-time defaults for the block-node install plan.
// Block-node's plan flows through the RSL strategy chain
// (default → config file → env → CLI flag), so its defaults live in Go
// rather than in pkg/software/infrastructure-catalog.yaml alongside the
// cluster-managed Helm charts.
package deps

const (
	BLOCK_NODE_NAMESPACE         = "block-node"
	BLOCK_NODE_RELEASE           = "block-node"
	BLOCK_NODE_CHART             = "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server"
	BLOCK_NODE_VERSION           = "0.41.0"
	BLOCK_NODE_STORAGE_BASE_PATH = "/mnt/fast-storage"
)

const (
	CONSENSUS_NODE_NAMESPACE = "hiero-network-1"

	// SOLO_OPERATOR_VERSION is the single source of truth for the solo-operator
	// chart + operator image version. Bump everything with one command —
	// `task bump:solo-operator VERSION=x` — which updates this constant, the Go API
	// module in go.mod (the CRD types), and the catalog checksum together, then
	// recompiles. The catalog's solo-operator default is injected from this value at
	// load time (see pkg/software/config.go), so the chart version and this constant
	// can never drift.
	SOLO_OPERATOR_VERSION = "0.6.1"

	CONSENSUS_NODE_UC_IMAGE = "ghcr.io/hashgraph/solo-operator/uc"
	// CONSENSUS_NODE_UC_VERSION is the UC (Update Coordinator) sidecar image tag.
	// The UC is the operator's OWN image, so it tracks the operator version by
	// default; give it its own literal only if the sidecar ever ships at a
	// different version than the chart.
	CONSENSUS_NODE_UC_VERSION = SOLO_OPERATOR_VERSION

	// CONSENSUS_NODE_IMAGE_PULL_SECRET Registry-agnostic name for the docker-registry pull secret. The images may be
	// served by any private registry (ghcr today, others later), so the secret name
	// does not hard-code a registry.
	CONSENSUS_NODE_IMAGE_PULL_SECRET = "private-registry-creds"
)
