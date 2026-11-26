// SPDX-License-Identifier: Apache-2.0

package alloy

import (
	"testing"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

var (
	testPromRemote = []models.AlloyRemoteConfig{{Name: "primary", URL: "http://prom:9090/api/v1/write", Username: "user"}}
	testLokiRemote = []models.AlloyRemoteConfig{{Name: "primary", URL: "http://loki:3100/loki/api/v1/push", Username: "user"}}
)

const (
	testLegacyPromURL = "http://prom:9090/api/v1/write"
	testLegacyLokiURL = "http://loki:3100/loki/api/v1/push"
)

func TestNewConfigBuilder_MonitorBlockNodeRejectedWithoutBothRemotes(t *testing.T) {
	tests := []struct {
		name    string
		cfg     models.AlloyConfig
		missing string
	}{
		{
			name:    "no remotes",
			cfg:     models.AlloyConfig{},
			missing: "missing: Prometheus, Loki",
		},
		{
			name:    "prometheus remote only",
			cfg:     models.AlloyConfig{PrometheusRemotes: testPromRemote},
			missing: "missing: Loki",
		},
		{
			name:    "loki remote only",
			cfg:     models.AlloyConfig{LokiRemotes: testLokiRemote},
			missing: "missing: Prometheus",
		},
		{
			name:    "legacy prometheus url only",
			cfg:     models.AlloyConfig{PrometheusURL: testLegacyPromURL},
			missing: "missing: Loki",
		},
		{
			name:    "legacy loki url only",
			cfg:     models.AlloyConfig{LokiURL: testLegacyLokiURL},
			missing: "missing: Prometheus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			cfg.ClusterName = "test-cluster"
			cfg.MonitorBlockNode = true

			cb, err := NewConfigBuilder(cfg, "")
			require.Error(t, err)
			assert.Nil(t, cb)
			assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), "got %v", err)
			assert.Equal(t,
				"block node monitoring needs a Prometheus remote and a Loki remote; "+tt.missing,
				errorx.Cast(err).Message())

			reason, ok := errx.ReasonOf(err)
			require.True(t, ok)
			assert.Equal(t, reasons.InvalidArgument, reason)
			hints, ok := errx.Hints(err)
			require.True(t, ok)
			assert.Equal(t, []string{
				"Pass both --add-prometheus-remote and --add-loki-remote with --monitor-block-node, " +
					"or set both alloy.prometheusRemotes and alloy.lokiRemotes in --config",
				"Or turn block node monitoring off: --monitor-block-node=false, or alloy.monitorBlockNode: false in --config",
			}, hints)
		})
	}
}

func TestNewConfigBuilder_MonitorBlockNodeAcceptedWithBothRemotes(t *testing.T) {
	tests := []struct {
		name string
		cfg  models.AlloyConfig
	}{
		{
			name: "prometheus and loki remotes",
			cfg:  models.AlloyConfig{PrometheusRemotes: testPromRemote, LokiRemotes: testLokiRemote},
		},
		{
			name: "legacy prometheus and loki urls",
			cfg:  models.AlloyConfig{PrometheusURL: testLegacyPromURL, LokiURL: testLegacyLokiURL},
		},
		{
			name: "prometheus remote and legacy loki url",
			cfg:  models.AlloyConfig{PrometheusRemotes: testPromRemote, LokiURL: testLegacyLokiURL},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			cfg.ClusterName = "test-cluster"
			cfg.MonitorBlockNode = true

			cb, err := NewConfigBuilder(cfg, "")
			require.NoError(t, err)

			modules, err := RenderModularConfigs(cb)
			require.NoError(t, err)
			assert.Contains(t, GetModuleNames(modules), "block-node")
		})
	}
}

func TestNewConfigBuilder_WithoutMonitorBlockNodeAcceptsAnyRemotes(t *testing.T) {
	tests := []struct {
		name string
		cfg  models.AlloyConfig
	}{
		{name: "no remotes", cfg: models.AlloyConfig{}},
		{name: "prometheus remote only", cfg: models.AlloyConfig{PrometheusRemotes: testPromRemote}},
		{name: "loki remote only", cfg: models.AlloyConfig{LokiRemotes: testLokiRemote}},
		{name: "both remotes", cfg: models.AlloyConfig{PrometheusRemotes: testPromRemote, LokiRemotes: testLokiRemote}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			cfg.ClusterName = "test-cluster"

			cb, err := NewConfigBuilder(cfg, "")
			require.NoError(t, err)

			modules, err := RenderModularConfigs(cb)
			require.NoError(t, err)
			assert.NotContains(t, GetModuleNames(modules), "block-node")
		})
	}
}
