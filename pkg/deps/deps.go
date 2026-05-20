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
	BLOCK_NODE_VERSION           = "0.30.2"
	BLOCK_NODE_STORAGE_BASE_PATH = "/mnt/fast-storage"
)
