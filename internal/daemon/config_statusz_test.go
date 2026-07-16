// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package daemon_test

import (
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDaemonConfig_BlockNodeStatuszBlock(t *testing.T) {
	content := `schemaVersion: 1
components:
  block_node:
    enabled: true
    kubeconfig: /opt/solo/weaver/config/daemon-bn.kubeconfig
    orbit: block-node
    monitors:
      traffic_shaper: true
    statusz:
      base_url: http://127.0.0.1:8080
      poll_interval: 3s
`
	path := writeTempConfig(t, content)

	cfg, err := daemon.LoadDaemonConfig(path)
	require.NoError(t, err)
	require.NotNil(t, cfg.Components.BlockNode)
	require.NotNil(t, cfg.Components.BlockNode.Statusz)
	assert.Equal(t, "http://127.0.0.1:8080", cfg.Components.BlockNode.Statusz.BaseURL)
	assert.Equal(t, 3*time.Second, cfg.Components.BlockNode.Statusz.EffectivePollInterval())
}

func TestStatuszConfig_EffectivePollInterval(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want time.Duration
	}{
		{"empty defaults to 5s", "", daemon.DefaultStatuszPollInterval},
		{"parses value", "10s", 10 * time.Second},
		{"unparseable falls back to default", "banana", daemon.DefaultStatuszPollInterval},
		{"non-positive falls back to default", "0s", daemon.DefaultStatuszPollInterval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := daemon.StatuszConfig{PollInterval: tt.in}
			assert.Equal(t, tt.want, s.EffectivePollInterval())
		})
	}
}

func TestStatuszConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     daemon.StatuszConfig
		wantErr bool
	}{
		{"empty is valid", daemon.StatuszConfig{}, false},
		{"http url + interval", daemon.StatuszConfig{BaseURL: "http://127.0.0.1:8080", PollInterval: "5s"}, false},
		{"https url", daemon.StatuszConfig{BaseURL: "https://bn.example:9000"}, false},
		{"missing scheme", daemon.StatuszConfig{BaseURL: "127.0.0.1:8080"}, true},
		{"non-http scheme", daemon.StatuszConfig{BaseURL: "ftp://host/x"}, true},
		{"no host", daemon.StatuszConfig{BaseURL: "http://"}, true},
		{"bad interval", daemon.StatuszConfig{PollInterval: "5"}, true},
		{"negative interval", daemon.StatuszConfig{PollInterval: "-5s"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, errorx.IsOfType(err, daemon.ErrConfigMalformed),
					"want ErrConfigMalformed, got %v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLoadDaemonConfig_RejectsBadStatuszURL(t *testing.T) {
	content := `schemaVersion: 1
components:
  block_node:
    enabled: true
    kubeconfig: /k
    orbit: block-node
    monitors:
      traffic_shaper: true
    statusz:
      base_url: "not a url with spaces"
`
	path := writeTempConfig(t, content)

	_, err := daemon.LoadDaemonConfig(path)
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, daemon.ErrConfigMalformed), "got %v", err)
}

func TestParseDaemonConfig_MatchesLoadFromFile(t *testing.T) {
	content := `schemaVersion: 1
components:
  block_node:
    enabled: true
    kubeconfig: /k
    orbit: block-node
    monitors:
      traffic_shaper: true
    statusz:
      base_url: http://127.0.0.1:8080
`
	path := writeTempConfig(t, content)

	fromFile, err := daemon.LoadDaemonConfig(path)
	require.NoError(t, err)
	fromBytes, err := daemon.ParseDaemonConfig([]byte(content), "in-memory")
	require.NoError(t, err)
	assert.Equal(t, fromFile, fromBytes)

	// Same validation as the file path: a malformed newer version is rejected.
	_, err = daemon.ParseDaemonConfig([]byte("schemaVersion: 99\ncomponents: {}\n"), "in-memory")
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, daemon.ErrConfigMalformed), "got %v", err)
}

func TestWriteDaemonConfig_RoundTripsStatusz(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/daemon.yaml"

	cfg := daemon.DaemonConfig{
		Components: daemon.DaemonComponents{
			BlockNode: &daemon.BlockNodeComponentConfig{
				Enabled:    true,
				Kubeconfig: "/opt/solo/weaver/config/daemon-bn.kubeconfig",
				Orbit:      "block-node",
				Monitors:   daemon.BlockNodeMonitors{TrafficShaper: true},
				Statusz:    &daemon.StatuszConfig{BaseURL: "http://127.0.0.1:8080", PollInterval: "5s"},
			},
		},
	}
	require.NoError(t, daemon.WriteDaemonConfig(path, cfg))

	got, err := daemon.LoadDaemonConfig(path)
	require.NoError(t, err)
	require.NotNil(t, got.Components.BlockNode.Statusz)
	assert.Equal(t, "http://127.0.0.1:8080", got.Components.BlockNode.Statusz.BaseURL)
	assert.Equal(t, "5s", got.Components.BlockNode.Statusz.PollInterval)
}
