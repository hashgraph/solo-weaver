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

// Consensus-node install-plan defaults. The consensus image repo/tag come from the
// signed deployment package (no compile-time default), so the defaults here are the
// dependency/version values weaver supplies itself:
//   - the UC (Update Coordinator) sidecar is the operator's OWN image (no operator
//     default), so its repo is fixed and its version tracks the operator/chart
//     version (keep in step with the solo-operator entry in
//     pkg/software/infrastructure-catalog.yaml).
//   - the image-pull secret is the docker-registry secret name the operator threads
//     onto the pods to pull private images.
const (
	CONSENSUS_NODE_NAMESPACE  = "hiero-network-1"
	CONSENSUS_NODE_UC_IMAGE   = "ghcr.io/hashgraph/solo-operator/uc"
	CONSENSUS_NODE_UC_VERSION = "0.6.0"
	// Registry-agnostic name for the docker-registry pull secret. The images may be
	// served by any private registry (ghcr today, others later), so the secret name
	// does not hard-code a registry.
	CONSENSUS_NODE_IMAGE_PULL_SECRET = "private-registry-creds"
)
