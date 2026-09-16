// SPDX-License-Identifier: Apache-2.0

// Package state provides cluster state discovery functions.
// TODO: This is a temporary implementation. It will be refactored into a proper
// state management system in a future PR.
package state

import (
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/pkg/helm"
)

const (
	// BlockNodeChartName is the chart name used to identify block node installations
	BlockNodeChartName = "block-node-server"
)

// GetBlockNodeNamespace discovers the namespace where block node is installed
// by querying Helm releases and matching the chart name.
// Returns the namespace if found, empty string if not installed, or an error if discovery failed.
func GetBlockNodeNamespace() (string, error) {
	hm, err := helm.NewManager()
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to create helm manager")
	}

	releases, err := hm.ListAll()
	if err != nil {
		return "", errorx.ExternalError.Wrap(err, "failed to list helm releases")
	}

	return blockNodeNamespaceFrom(releases), nil
}

// blockNodeNamespaceFrom returns the namespace of the deployed block node release,
// or "" when there is none. ListAll reports releases in every state, so a failed or
// uninstalled record would otherwise name a namespace holding no block node.
// Extracted from GetBlockNodeNamespace so it can be unit-tested without a cluster.
func blockNodeNamespaceFrom(releases []*release.Release) string {
	for _, rel := range releases {
		if rel == nil || rel.Info == nil || rel.Info.Status != release.StatusDeployed {
			continue
		}
		if rel.Chart != nil && rel.Chart.Metadata != nil && rel.Chart.Metadata.Name == BlockNodeChartName {
			return rel.Namespace
		}
	}

	return ""
}
