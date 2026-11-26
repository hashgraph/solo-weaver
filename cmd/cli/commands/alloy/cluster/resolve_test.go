// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/alloy"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
)

const (
	testFilePromURL = "http://file-prom:9090/api/v1/write"
	testFileLokiURL = "http://file-loki:3100/loki/api/v1/push"
	testFlagPromURL = "http://flag-prom:9090/api/v1/write"
	testFlagLokiURL = "http://flag-loki:3100/loki/api/v1/push"
)

var (
	filePromList = []models.AlloyRemoteConfig{{Name: "fileprom", URL: testFilePromURL, Username: "u", LabelProfile: "ops"}}
	fileLokiList = []models.AlloyRemoteConfig{{Name: "fileloki", URL: testFileLokiURL, Username: "u", LabelProfile: "ops"}}
	flagPromList = []models.AlloyRemoteConfig{{Name: "flagprom", URL: testFlagPromURL}}
	flagLokiList = []models.AlloyRemoteConfig{{Name: "flagloki", URL: testFlagLokiURL}}
)

// documentedFile is the alloy block from docs/reference/configuration.md.
func documentedFile() models.AlloyConfig {
	return models.AlloyConfig{
		MonitorBlockNode:  true,
		ClusterName:       "mainnet-block-01",
		PrometheusRemotes: filePromList,
		LokiRemotes:       fileLokiList,
	}
}

func TestResolveAlloyConfig(t *testing.T) {
	tests := []struct {
		name       string
		file       models.AlloyConfig
		flags      models.AlloyConfig
		monitorSet bool
		want       models.AlloyConfig
		// checkRender builds and renders the merged config, then checks the block-node module.
		checkRender         bool
		wantBlockNodeModule bool
	}{
		{
			name:                "file only: remotes and block node monitoring come from the file",
			file:                documentedFile(),
			want:                documentedFile(),
			checkRender:         true,
			wantBlockNodeModule: true,
		},
		{
			name:  "--add-prometheus-remote replaces the file's Prometheus list, Loki stays",
			file:  documentedFile(),
			flags: models.AlloyConfig{PrometheusRemotes: flagPromList},
			want: models.AlloyConfig{
				MonitorBlockNode: true, ClusterName: "mainnet-block-01",
				PrometheusRemotes: flagPromList, LokiRemotes: fileLokiList,
			},
			checkRender:         true,
			wantBlockNodeModule: true,
		},
		{
			name:  "--add-loki-remote replaces the file's Loki list, Prometheus stays",
			file:  documentedFile(),
			flags: models.AlloyConfig{LokiRemotes: flagLokiList},
			want: models.AlloyConfig{
				MonitorBlockNode: true, ClusterName: "mainnet-block-01",
				PrometheusRemotes: filePromList, LokiRemotes: flagLokiList,
			},
			checkRender:         true,
			wantBlockNodeModule: true,
		},
		{
			name:       "--monitor-block-node=false turns off the file's true",
			file:       documentedFile(),
			flags:      models.AlloyConfig{MonitorBlockNode: false},
			monitorSet: true,
			want: models.AlloyConfig{
				MonitorBlockNode: false, ClusterName: "mainnet-block-01",
				PrometheusRemotes: filePromList, LokiRemotes: fileLokiList,
			},
			checkRender: true,
		},
		{
			name:                "--monitor-block-node not passed keeps the file's true",
			file:                documentedFile(),
			flags:               models.AlloyConfig{MonitorBlockNode: false},
			want:                documentedFile(),
			checkRender:         true,
			wantBlockNodeModule: true,
		},
		{
			name:       "--monitor-block-node turns on over the file's false",
			file:       models.AlloyConfig{PrometheusRemotes: filePromList, LokiRemotes: fileLokiList},
			flags:      models.AlloyConfig{MonitorBlockNode: true},
			monitorSet: true,
			want: models.AlloyConfig{
				MonitorBlockNode: true, PrometheusRemotes: filePromList, LokiRemotes: fileLokiList,
			},
			checkRender:         true,
			wantBlockNodeModule: true,
		},
		{
			name:        "no --config and no flags: no remotes, no block node monitoring",
			want:        models.AlloyConfig{},
			checkRender: true,
		},
		{
			name:  "non-empty string flags win, empty ones keep the file",
			file:  models.AlloyConfig{ClusterName: "file-cluster", Environment: "staging", ClusterSecretStoreName: "file-store"},
			flags: models.AlloyConfig{ClusterName: "flag-cluster"},
			want:  models.AlloyConfig{ClusterName: "flag-cluster", Environment: "staging", ClusterSecretStoreName: "file-store"},
		},
		{
			name:  "--prometheus-url replaces the file's Prometheus list",
			file:  documentedFile(),
			flags: models.AlloyConfig{PrometheusURL: testFlagPromURL, PrometheusUsername: "flaguser"},
			want: models.AlloyConfig{
				MonitorBlockNode: true, ClusterName: "mainnet-block-01",
				PrometheusURL: testFlagPromURL, PrometheusUsername: "flaguser", LokiRemotes: fileLokiList,
			},
			checkRender:         true,
			wantBlockNodeModule: true,
		},
		{
			name:  "--add-prometheus-remote drops the file's legacy Prometheus keys",
			file:  models.AlloyConfig{PrometheusURL: testFilePromURL, PrometheusUsername: "fileuser", LokiURL: testFileLokiURL},
			flags: models.AlloyConfig{PrometheusRemotes: flagPromList},
			want:  models.AlloyConfig{PrometheusRemotes: flagPromList, LokiURL: testFileLokiURL},
		},
		{
			name: "legacy keys from the file are kept when no remote flag is given",
			file: models.AlloyConfig{PrometheusURL: testFilePromURL, PrometheusUsername: "fileuser", LokiURL: testFileLokiURL},
			want: models.AlloyConfig{PrometheusURL: testFilePromURL, PrometheusUsername: "fileuser", LokiURL: testFileLokiURL},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveAlloyConfig(tt.file, tt.flags, tt.monitorSet)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			if tt.checkRender {
				cb, err := alloy.NewConfigBuilder(got, "")
				require.NoError(t, err)
				modules, err := alloy.RenderModularConfigs(cb)
				require.NoError(t, err)
				names := alloy.GetModuleNames(modules)
				assert.Equal(t, tt.wantBlockNodeModule, slices.Contains(names, "block-node"), "modules: %v", names)
			}
		})
	}
}

// The #1491 check runs on the merged config, so a file with
// monitorBlockNode: true and one remote type fails in preflight.
func TestResolveAlloyConfig_FileMonitorBlockNodeNeedsBothRemotes(t *testing.T) {
	file := models.AlloyConfig{ClusterName: "c1", MonitorBlockNode: true, PrometheusRemotes: filePromList}

	got, err := resolveAlloyConfig(file, models.AlloyConfig{}, false)
	require.NoError(t, err)

	_, err = alloy.NewConfigBuilder(got, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing: Loki")
}

func TestResolveAlloyConfig_Rejects(t *testing.T) {
	tests := []struct {
		name      string
		file      models.AlloyConfig
		flags     models.AlloyConfig
		wantMsg   string
		wantHints []string
	}{
		{
			name:    "legacy and new flags mixed",
			flags:   models.AlloyConfig{PrometheusURL: testFlagPromURL, LokiRemotes: flagLokiList},
			wantMsg: "cannot mix legacy flags (--prometheus-url, --prometheus-username, --loki-url, --loki-username) with new flags (--add-prometheus-remote, --add-loki-remote); use one style or the other",
		},
		{
			name:    "--prometheus-username without --prometheus-url",
			file:    models.AlloyConfig{PrometheusURL: testFilePromURL},
			flags:   models.AlloyConfig{PrometheusUsername: "flaguser"},
			wantMsg: "--prometheus-username needs --prometheus-url",
			wantHints: []string{
				"Pass --prometheus-url with --prometheus-username",
				"Or drop --prometheus-username and set alloy.prometheusUsername next to alloy.prometheusUrl in --config",
				"Or use --add-prometheus-remote=name=<name>,url=<url>,username=<username>",
			},
		},
		{
			name:    "--loki-username without --loki-url",
			flags:   models.AlloyConfig{LokiUsername: "flaguser"},
			wantMsg: "--loki-username needs --loki-url",
			wantHints: []string{
				"Pass --loki-url with --loki-username",
				"Or drop --loki-username and set alloy.lokiUsername next to alloy.lokiUrl in --config",
				"Or use --add-loki-remote=name=<name>,url=<url>,username=<username>",
			},
		},
		{
			name:    "file sets a Prometheus list and the legacy URL",
			file:    models.AlloyConfig{PrometheusRemotes: filePromList, PrometheusURL: testFilePromURL},
			wantMsg: "--config sets both alloy.prometheusRemotes and the deprecated alloy.prometheusUrl/alloy.prometheusUsername",
			wantHints: []string{
				"Remove alloy.prometheusUrl and alloy.prometheusUsername from --config; alloy.prometheusRemotes replaces them",
			},
		},
		{
			name:    "file sets a Loki list and the legacy username",
			file:    models.AlloyConfig{LokiRemotes: fileLokiList, LokiUsername: "fileuser"},
			wantMsg: "--config sets both alloy.lokiRemotes and the deprecated alloy.lokiUrl/alloy.lokiUsername",
			wantHints: []string{
				"Remove alloy.lokiUrl and alloy.lokiUsername from --config; alloy.lokiRemotes replaces them",
			},
		},
		{
			name:    "file sets the legacy Prometheus username without the URL",
			file:    models.AlloyConfig{PrometheusUsername: "fileuser"},
			wantMsg: "--config sets alloy.prometheusUsername without alloy.prometheusUrl",
			wantHints: []string{
				"Add alloy.prometheusUrl to --config, or list the remote under alloy.prometheusRemotes",
			},
		},
		{
			name:    "file sets the legacy Loki username without the URL",
			file:    models.AlloyConfig{LokiUsername: "fileuser"},
			wantMsg: "--config sets alloy.lokiUsername without alloy.lokiUrl",
			wantHints: []string{
				"Add alloy.lokiUrl to --config, or list the remote under alloy.lokiRemotes",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveAlloyConfig(tt.file, tt.flags, false)
			require.Error(t, err)
			assert.Equal(t, models.AlloyConfig{}, got)
			assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), "got %v", err)
			assert.Equal(t, tt.wantMsg, errorx.Cast(err).Message())

			if tt.wantHints == nil {
				return
			}
			reason, ok := errx.ReasonOf(err)
			require.True(t, ok)
			assert.Equal(t, reasons.InvalidArgument, reason)
			hints, ok := errx.Hints(err)
			require.True(t, ok)
			assert.Equal(t, tt.wantHints, hints)
		})
	}
}

// A flag for one type and the file for the other is not mixing styles.
func TestResolveAlloyConfig_CrossTypeStylesAllowed(t *testing.T) {
	got, err := resolveAlloyConfig(
		models.AlloyConfig{PrometheusRemotes: filePromList},
		models.AlloyConfig{LokiURL: testFlagLokiURL}, false)
	require.NoError(t, err)
	assert.Equal(t, models.AlloyConfig{PrometheusRemotes: filePromList, LokiURL: testFlagLokiURL}, got)
}

// loadConfigFile loads path through the real --config loader and returns its alloy block.
func loadConfigFile(t *testing.T, path string) models.AlloyConfig {
	t.Helper()
	saved := config.Get()
	t.Cleanup(func() {
		_ = config.Initialize("")
		_ = config.Set(&saved)
	})
	require.NoError(t, config.Initialize(path))
	return config.Get().Alloy
}

// The configuration reference's example must install as documented.
func TestResolveAlloyConfig_DocumentedExample(t *testing.T) {
	doc, err := os.ReadFile("../../../../../docs/reference/configuration.md")
	require.NoError(t, err)
	block := regexp.MustCompile("(?s)```yaml\n# config.yaml\n(.*?)```").FindSubmatch(doc)
	require.NotNil(t, block, "config.yaml example not found in configuration.md")
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, block[1], 0o600))

	got, err := resolveAlloyConfig(loadConfigFile(t, path), models.AlloyConfig{}, false)
	require.NoError(t, err)
	require.NoError(t, got.Validate())
	assert.True(t, got.MonitorBlockNode)
	assert.Len(t, got.PrometheusRemotes, 1)
	assert.Len(t, got.LokiRemotes, 1)

	cb, err := alloy.NewConfigBuilder(got, "mainnet")
	require.NoError(t, err)
	modules, err := alloy.RenderModularConfigs(cb)
	require.NoError(t, err)
	assert.Contains(t, alloy.GetModuleNames(modules), "block-node")
}

func TestResolveAlloyConfig_LocalDevConfigLoads(t *testing.T) {
	got, err := resolveAlloyConfig(loadConfigFile(t, "../../../../../test/alloy/config-local-dev.yaml"), models.AlloyConfig{}, false)
	require.NoError(t, err)
	require.NoError(t, got.Validate())
	assert.NotEmpty(t, got.PrometheusURL)
	assert.NotEmpty(t, got.LokiURL)
}
