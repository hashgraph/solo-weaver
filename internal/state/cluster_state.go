// SPDX-License-Identifier: Apache-2.0

// Package state provides cluster state discovery functions.
// TODO: This is a temporary implementation. It will be refactored into a proper
// state management system in a future PR.
package state

import (
	"github.com/joomcode/errorx"

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

	for _, rel := range releases {
		if rel.Chart != nil && rel.Chart.Metadata != nil {
			if rel.Chart.Metadata.Name == BlockNodeChartName {
				return rel.Namespace, nil
			}
		}
	}

	return "", nil
}
