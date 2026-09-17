// SPDX-License-Identifier: Apache-2.0

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
)

func helmRelease(namespace, chartName string, status release.Status) *release.Release {
	return &release.Release{
		Name:      "block-node",
		Namespace: namespace,
		Info:      &release.Info{Status: status},
		Chart:     &chart.Chart{Metadata: &chart.Metadata{Name: chartName}},
	}
}

func Test_blockNodeNamespaceFrom(t *testing.T) {
	tests := []struct {
		name     string
		releases []*release.Release
		want     string
	}{
		{
			name:     "no releases",
			releases: nil,
			want:     "",
		},
		{
			name:     "deployed block node",
			releases: []*release.Release{helmRelease("block-node", BlockNodeChartName, release.StatusDeployed)},
			want:     "block-node",
		},
		{
			name:     "failed block node is not installed",
			releases: []*release.Release{helmRelease("block-node", BlockNodeChartName, release.StatusFailed)},
			want:     "",
		},
		{
			name:     "uninstalled block node is not installed",
			releases: []*release.Release{helmRelease("block-node", BlockNodeChartName, release.StatusUninstalled)},
			want:     "",
		},
		{
			name: "a stale failed release never shadows the deployed one",
			releases: []*release.Release{
				helmRelease("block-node-old", BlockNodeChartName, release.StatusFailed),
				helmRelease("block-node", BlockNodeChartName, release.StatusDeployed),
			},
			want: "block-node",
		},
		{
			name:     "another chart is ignored",
			releases: []*release.Release{helmRelease("block-node", "some-other-chart", release.StatusDeployed)},
			want:     "",
		},
		{
			name:     "nil entries and missing metadata are skipped",
			releases: []*release.Release{nil, {Info: &release.Info{Status: release.StatusDeployed}}},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, blockNodeNamespaceFrom(tt.releases))
		})
	}
}
