// SPDX-License-Identifier: Apache-2.0

// Integration tests for Helm manager that require a running cluster.
//
// Build Tags: helm_integration OR require_cluster
//
// These tests are NOT part of the standard `integration` test suite.
// They run in Phase 2 of the Taskfile `test:integration:verbose` task,
// after the cluster has been created in Phase 1:
//
//   Phase 1: go test -tags='cluster_setup' -run '^Test_ClusterSetup$' ./internal/workflows/...
//            → Creates the Kubernetes cluster
//
//   Phase 2: go test -tags='require_cluster' ./...
//            → Runs these tests (matched via `require_cluster` tag)
//
//   Phase 3: go test -tags='integration' ./...
//            → Runs general integration tests
//
//   Phase 4: go test -tags='cluster_setup' ./internal/workflows/...
//            → Tears down the cluster
//
// Dependencies:
//   - Requires a running Kubernetes cluster (created by Phase 1)
//   - Requires valid kubeconfig
//
// To run these tests standalone (with an existing cluster):
//   go test -v -tags='require_cluster' ./pkg/helm/...
//   # or
//   go test -v -tags='helm_integration' ./pkg/helm/...

//go:build helm_integration || require_cluster

package helm

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/cli/values"
)

// testChart groups constants for a Helm chart to test
type testChartInfo struct {
	RepoName    string
	RepoURL     string
	ReleaseName string
	ChartRef    string
	ChartVer    string
	Namespace   string
	Timeout     time.Duration
}

var testChart = testChartInfo{
	RepoName:    "podinfo",
	RepoURL:     "https://stefanprodan.github.io/podinfo",
	ReleaseName: "podinfo-test",
	ChartRef:    "podinfo/podinfo",
	ChartVer:    "6.3.3",
	Namespace:   "podinfo-system-test",
	Timeout:     DefaultTimeout,
}

// helper to create Helm manager
func newTestManager(t *testing.T) Manager {
	log := zerolog.Nop()
	manager, err := NewManager(WithLogger(log))
	require.NoError(t, err)
	return manager
}

func TestHelmManager_Integration(t *testing.T) {
	manager := newTestManager(t)
	ctx := context.Background()

	if exists, err := manager.IsInstalled(testChart.ReleaseName, testChart.Namespace); err == nil && exists {
		err := manager.UninstallChart(testChart.ReleaseName, testChart.Namespace)
		require.NoError(t, err)
	}

	t.Run("AddRepo", func(t *testing.T) {
		repoObj, err := manager.AddRepo(testChart.RepoName, testChart.RepoURL, RepoAddOptions{})
		require.NoError(t, err)
		require.NotNil(t, repoObj)
		require.Equal(t, testChart.RepoName, repoObj.Config.Name)
		require.Equal(t, testChart.RepoURL, repoObj.Config.URL)
	})

	t.Run("InstallChart", func(t *testing.T) {
		rel, err := manager.InstallChart(ctx, testChart.ReleaseName, testChart.ChartRef, testChart.ChartVer, testChart.Namespace, InstallChartOptions{
			ValueOpts: &values.Options{
				Values: []string{"ui.message=hello1"},
			},
			Atomic:          true,
			Wait:            true,
			Timeout:         testChart.Timeout,
			CreateNamespace: true,
		})
		require.NoError(t, err)
		require.NotNil(t, rel)
		require.Equal(t, testChart.ReleaseName, rel.Name)
	})

	t.Run("GetRelease", func(t *testing.T) {
		rel, err := manager.GetRelease(testChart.ReleaseName, testChart.Namespace)
		require.NoError(t, err)
		require.Equal(t, testChart.ReleaseName, rel.Name)
	})

	t.Run("IsInstalled", func(t *testing.T) {
		installed, err := manager.IsInstalled(testChart.ReleaseName, testChart.Namespace)
		require.NoError(t, err)
		require.True(t, installed)
	})

	t.Run("UpgradeChart", func(t *testing.T) {
		rel, err := manager.UpgradeChart(ctx, testChart.ReleaseName, testChart.ChartRef, testChart.ChartVer, testChart.Namespace, UpgradeChartOptions{
			ValueOpts: &values.Options{
				Values: []string{"ui.message=hello2"},
			},
			Atomic:      true,
			Wait:        true,
			ReuseValues: true,
			Timeout:     testChart.Timeout,
		})
		require.NoError(t, err)
		require.NotNil(t, rel)
		require.Equal(t, testChart.ReleaseName, rel.Name)
	})

	t.Run("DeployChart_InstallOrUpgrade", func(t *testing.T) {
		rel, err := manager.DeployChart(ctx, testChart.ReleaseName, testChart.ChartRef, testChart.ChartVer, testChart.Namespace, DeployChartOptions{
			ValueOpts: &values.Options{
				Values: []string{"speaker.frr.enabled=false"},
			},
			CreateNamespace: true,
			ReuseValues:     true,
			Atomic:          true,
			Wait:            true,
			Timeout:         testChart.Timeout,
		})
		require.NoError(t, err)
		require.NotNil(t, rel)
		require.Equal(t, testChart.ReleaseName, rel.Name)
	})

	t.Run("ListAndListAll", func(t *testing.T) {
		releases, err := manager.List(testChart.Namespace, false)
		require.NoError(t, err)
		require.Len(t, releases, 1)

		allReleases, err := manager.ListAll()
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(allReleases), 1)
	})

	t.Run("UninstallChart", func(t *testing.T) {
		err := manager.UninstallChart(testChart.ReleaseName, testChart.Namespace)
		require.NoError(t, err)

		installed, err := manager.IsInstalled(testChart.ReleaseName, testChart.Namespace)
		require.NoError(t, err)
		require.False(t, installed)
	})

	t.Run("InstallInvalidChart", func(t *testing.T) {
		_, err := manager.InstallChart(ctx, "bad-release", "invalid/chart", "0.0.1", "default", InstallChartOptions{
			Atomic:  true,
			Wait:    true,
			Timeout: testChart.Timeout,
		})
		require.Error(t, err)
	})

	t.Run("UpgradeNonExistentRelease", func(t *testing.T) {
		_, err := manager.UpgradeChart(ctx, "nonexistent", testChart.ChartRef, testChart.ChartVer, testChart.Namespace, UpgradeChartOptions{
			ReuseValues: true,
			Atomic:      true,
			Wait:        true,
			Timeout:     testChart.Timeout,
		})
		require.Error(t, err)
	})
}
