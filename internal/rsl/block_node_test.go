// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// MockRealityChecker is a mock implementation of reality.Checker.
// Return types must exactly match the interface: value types, not pointers.
type MockRealityChecker struct {
	mock.Mock
}

func (m *MockRealityChecker) ClusterState(_ context.Context) (state.ClusterState, error) {
	args := m.Called()
	return args.Get(0).(state.ClusterState), args.Error(1)
}

func (m *MockRealityChecker) MachineState(_ context.Context) (state.MachineState, error) {
	args := m.Called()
	return args.Get(0).(state.MachineState), args.Error(1)
}

func (m *MockRealityChecker) BlockNodeState(_ context.Context) (state.BlockNodeState, error) {
	args := m.Called()
	return args.Get(0).(state.BlockNodeState), args.Error(1)
}

// newTestRuntime is a test helper that creates a BlockNodeRuntimeState via NewBlockNodeRuntime.
func newTestRuntime(t *testing.T, cfg models.Config, st state.BlockNodeState, checker *MockRealityChecker) *BlockNodeRuntimeState {
	t.Helper()
	br, err := NewBlockNodeRuntime(cfg, st, checker, 5*time.Second)
	require.NoError(t, err)
	return br
}

func TestNewBlockNodeRuntime(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		cfg := models.Config{
			BlockNode: models.BlockNodeConfig{
				Release:      "test-release",
				Namespace:    "test-namespace",
				Version:      "v1.0.0",
				ChartName:    "block-node",
				Chart:        "repo/chart",
				ChartVersion: "1.0.0",
			},
		}
		br, err := NewBlockNodeRuntime(cfg, state.BlockNodeState{}, new(MockRealityChecker), 5*time.Second)
		require.NoError(t, err)
		assert.NotNil(t, br)
	})

	t.Run("NilRealityChecker", func(t *testing.T) {
		_, err := NewBlockNodeRuntime(models.Config{}, state.BlockNodeState{}, nil, 5*time.Second)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reality checker cannot be nil")
	})

	t.Run("IndependentInstances", func(t *testing.T) {
		br1, err := NewBlockNodeRuntime(models.Config{}, state.BlockNodeState{}, new(MockRealityChecker), 5*time.Second)
		require.NoError(t, err)
		br2, err := NewBlockNodeRuntime(models.Config{}, state.BlockNodeState{}, new(MockRealityChecker), 5*time.Second)
		require.NoError(t, err)
		assert.NotSame(t, br1, br2, "NewBlockNodeRuntime should return independent instances")
	})
}

func TestBlockNodeRuntime_SetBlockNodeConfig(t *testing.T) {
	cfg := models.Config{
		BlockNode: models.BlockNodeConfig{
			Release:      "initial-release",
			Namespace:    "initial-namespace",
			Version:      "v1.0.0",
			ChartName:    "block-node",
			Chart:        "repo/chart",
			ChartVersion: "1.0.0",
		},
	}
	br := newTestRuntime(t, cfg, state.BlockNodeState{}, new(MockRealityChecker))

	t.Run("Success", func(t *testing.T) {
		err := br.SetUserInputs(models.BlocknodeInputs{
			Release:      "new-release",
			Namespace:    "new-namespace",
			Version:      "v2.0.0",
			ChartName:    "new-chart",
			Chart:        "new-repo/chart",
			ChartVersion: "2.0.0",
			Storage:      models.BlockNodeStorage{LiveSize: "10Gi"},
		})
		require.NoError(t, err)
	})
}

func TestBlockNodeRuntime_SetReleaseName(t *testing.T) {
	br := newTestRuntime(t, models.Config{
		BlockNode: models.BlockNodeConfig{Release: "test-release"},
	}, state.BlockNodeState{}, new(MockRealityChecker))

	t.Run("Success", func(t *testing.T) {
		require.NoError(t, br.setReleaseNameInput("new-release"))
	})
	t.Run("EmptyString", func(t *testing.T) {
		require.NoError(t, br.setReleaseNameInput(""))
	})
}

func TestBlockNodeRuntime_SetNamespace(t *testing.T) {
	br := newTestRuntime(t, models.Config{
		BlockNode: models.BlockNodeConfig{Namespace: "default"},
	}, state.BlockNodeState{}, new(MockRealityChecker))

	t.Run("Success", func(t *testing.T) {
		require.NoError(t, br.setNamespaceInput("production"))
	})
}

func TestBlockNodeRuntime_SetVersion(t *testing.T) {
	br := newTestRuntime(t, models.Config{
		BlockNode: models.BlockNodeConfig{Version: "v1.0.0"},
	}, state.BlockNodeState{}, new(MockRealityChecker))

	t.Run("Success", func(t *testing.T) {
		require.NoError(t, br.setVersionInput("v2.0.0"))
	})
}

func TestBlockNodeRuntime_SetChartName(t *testing.T) {
	br := newTestRuntime(t, models.Config{
		BlockNode: models.BlockNodeConfig{ChartName: "block-node"},
	}, state.BlockNodeState{}, new(MockRealityChecker))

	t.Run("Success", func(t *testing.T) {
		require.NoError(t, br.setChartNameInput("new-chart"))
	})
}

func TestBlockNodeRuntime_SetChartRef(t *testing.T) {
	br := newTestRuntime(t, models.Config{
		BlockNode: models.BlockNodeConfig{Chart: "repo/chart"},
	}, state.BlockNodeState{}, new(MockRealityChecker))

	t.Run("Success", func(t *testing.T) {
		require.NoError(t, br.setChartRefInput("new-repo/new-chart"))
	})
}

func TestBlockNodeRuntime_SetChartVersion(t *testing.T) {
	br := newTestRuntime(t, models.Config{
		BlockNode: models.BlockNodeConfig{ChartVersion: "1.0.0"},
	}, state.BlockNodeState{}, new(MockRealityChecker))

	t.Run("Success", func(t *testing.T) {
		require.NoError(t, br.setChartVersionInput("2.0.0"))
	})
}

func TestBlockNodeRuntime_SetStorage(t *testing.T) {
	br := newTestRuntime(t, models.Config{
		BlockNode: models.BlockNodeConfig{
			Storage: models.BlockNodeStorage{LiveSize: "5Gi"},
		},
	}, state.BlockNodeState{}, new(MockRealityChecker))

	t.Run("Success", func(t *testing.T) {
		require.NoError(t, br.setStorageInput(models.BlockNodeStorage{LiveSize: "10Gi"}))
	})
}

func TestBlockNodeRuntime_Getters(t *testing.T) {
	cfg := models.Config{
		BlockNode: models.BlockNodeConfig{
			Release:      "test-release",
			Namespace:    "test-namespace",
			Version:      "v1.0.0",
			ChartName:    "block-node",
			Chart:        "repo/chart",
			ChartVersion: "1.0.0",
			Storage:      models.BlockNodeStorage{LiveSize: "5Gi"},
		},
	}
	st := state.BlockNodeState{
		ReleaseInfo: state.HelmReleaseInfo{
			Name:         "test-release",
			Namespace:    "test-namespace",
			Version:      "v1.0.0",
			ChartName:    "block-node",
			ChartRef:     "repo/chart",
			ChartVersion: "1.0.0",
			Status:       release.StatusDeployed,
		},
	}
	br := newTestRuntime(t, cfg, st, new(MockRealityChecker))

	t.Run("Namespace", func(t *testing.T) {
		val, err := br.Namespace()
		require.NoError(t, err)
		assert.NotNil(t, val)
	})
	t.Run("ReleaseName", func(t *testing.T) {
		val, err := br.ReleaseName()
		require.NoError(t, err)
		assert.NotNil(t, val)
	})
	t.Run("Version", func(t *testing.T) {
		val, err := br.Version()
		require.NoError(t, err)
		assert.NotNil(t, val)
	})
	t.Run("ChartName", func(t *testing.T) {
		val, err := br.ChartName()
		require.NoError(t, err)
		assert.NotNil(t, val)
	})
	t.Run("ChartRepo", func(t *testing.T) {
		val, err := br.ChartRepo()
		require.NoError(t, err)
		assert.NotNil(t, val)
	})
	t.Run("ChartVersion", func(t *testing.T) {
		val, err := br.ChartVersion()
		require.NoError(t, err)
		assert.NotNil(t, val)
	})
	t.Run("Storage", func(t *testing.T) {
		val, err := br.Storage()
		require.NoError(t, err)
		assert.NotNil(t, val)
	})
}
