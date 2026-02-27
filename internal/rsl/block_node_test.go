// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// MockRealityChecker is a mock implementation of reality.Checker
type MockRealityChecker struct {
	mock.Mock
}

func (m *MockRealityChecker) RefreshState(ctx context.Context, st *models.State) error {
	return nil
}

func (m *MockRealityChecker) MachineState(ctx context.Context) (*models.MachineState, error) {
	return nil, nil
}

func (m *MockRealityChecker) BlockNodeState(ctx context.Context) (*models.BlockNodeState, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.BlockNodeState), args.Error(1)
}

func (m *MockRealityChecker) ClusterState(ctx context.Context) (*models.ClusterState, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.ClusterState), args.Error(1)
}

func TestInitBlockNodeRuntime(t *testing.T) {
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
		state := models.BlockNodeState{}
		mockChecker := new(MockRealityChecker)
		refreshInterval := 5 * time.Second

		err := InitBlockNodeRuntime(cfg, state, mockChecker, refreshInterval)
		require.NoError(t, err)
		assert.NotNil(t, BlockNode())
	})

	t.Run("NilRealityChecker", func(t *testing.T) {
		cfg := models.Config{}
		state := models.BlockNodeState{}

		err := InitBlockNodeRuntime(cfg, state, nil, 5*time.Second)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reality checker cannot be nil")
	})
}

func TestBlockNodeRuntime_SetBlockNodeConfig(t *testing.T) {
	setupRuntime := func() (*BlockNodeRuntimeState, *MockRealityChecker) {
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
		state := models.BlockNodeState{}
		mockChecker := new(MockRealityChecker)

		err := InitBlockNodeRuntime(cfg, state, mockChecker, 5*time.Second)
		require.NoError(t, err)

		return BlockNode(), mockChecker
	}

	t.Run("Success", func(t *testing.T) {
		br, _ := setupRuntime()

		newCfg := models.BlocknodeInputs{
			Release:      "new-release",
			Namespace:    "new-namespace",
			Version:      "v2.0.0",
			ChartName:    "new-chart",
			Chart:        "new-repo/chart",
			ChartVersion: "2.0.0",
			Storage: models.BlockNodeStorage{
				LiveSize: "10Gi",
			},
		}

		err := br.SetUserInputs(newCfg)
		require.NoError(t, err)
	})
}

func TestBlockNodeRuntime_SetReleaseName(t *testing.T) {
	cfg := models.Config{
		BlockNode: models.BlockNodeConfig{
			Release: "test-release",
		},
	}
	state := models.BlockNodeState{}
	mockChecker := new(MockRealityChecker)

	err := InitBlockNodeRuntime(cfg, state, mockChecker, 5*time.Second)
	require.NoError(t, err)
	br := BlockNode()

	t.Run("Success", func(t *testing.T) {
		err := br.setReleaseNameInput("new-release")
		require.NoError(t, err)
	})

	t.Run("EmptyString", func(t *testing.T) {
		err := br.setReleaseNameInput("")
		require.NoError(t, err)
	})
}

func TestBlockNodeRuntime_SetNamespace(t *testing.T) {
	cfg := models.Config{
		BlockNode: models.BlockNodeConfig{
			Namespace: "default",
		},
	}
	state := models.BlockNodeState{}
	mockChecker := new(MockRealityChecker)

	err := InitBlockNodeRuntime(cfg, state, mockChecker, 5*time.Second)
	require.NoError(t, err)
	br := BlockNode()

	t.Run("Success", func(t *testing.T) {
		err := br.setNamespaceInput("production")
		require.NoError(t, err)
	})
}

func TestBlockNodeRuntime_SetVersion(t *testing.T) {
	cfg := models.Config{
		BlockNode: models.BlockNodeConfig{
			Version: "v1.0.0",
		},
	}
	state := models.BlockNodeState{}
	mockChecker := new(MockRealityChecker)

	err := InitBlockNodeRuntime(cfg, state, mockChecker, 5*time.Second)
	require.NoError(t, err)
	br := BlockNode()

	t.Run("Success", func(t *testing.T) {
		err := br.setVersionInput("v2.0.0")
		require.NoError(t, err)
	})
}

func TestBlockNodeRuntime_SetChartName(t *testing.T) {
	cfg := models.Config{
		BlockNode: models.BlockNodeConfig{
			ChartName: "block-node",
		},
	}
	state := models.BlockNodeState{}
	mockChecker := new(MockRealityChecker)

	err := InitBlockNodeRuntime(cfg, state, mockChecker, 5*time.Second)
	require.NoError(t, err)
	br := BlockNode()

	t.Run("Success", func(t *testing.T) {
		err := br.setChartNameInput("new-chart")
		require.NoError(t, err)
	})
}

func TestBlockNodeRuntime_SetChartRef(t *testing.T) {
	cfg := models.Config{
		BlockNode: models.BlockNodeConfig{
			Chart: "repo/chart",
		},
	}
	state := models.BlockNodeState{}
	mockChecker := new(MockRealityChecker)

	err := InitBlockNodeRuntime(cfg, state, mockChecker, 5*time.Second)
	require.NoError(t, err)
	br := BlockNode()

	t.Run("Success", func(t *testing.T) {
		err := br.setChartRefInput("new-repo/new-chart")
		require.NoError(t, err)
	})
}

func TestBlockNodeRuntime_SetChartVersion(t *testing.T) {
	cfg := models.Config{
		BlockNode: models.BlockNodeConfig{
			ChartVersion: "1.0.0",
		},
	}
	state := models.BlockNodeState{}
	mockChecker := new(MockRealityChecker)

	err := InitBlockNodeRuntime(cfg, state, mockChecker, 5*time.Second)
	require.NoError(t, err)
	br := BlockNode()

	t.Run("Success", func(t *testing.T) {
		err := br.setChartVersionInput("2.0.0")
		require.NoError(t, err)
	})
}

func TestBlockNodeRuntime_SetStorage(t *testing.T) {
	cfg := models.Config{
		BlockNode: models.BlockNodeConfig{
			Storage: models.BlockNodeStorage{
				LiveSize: "5Gi",
			},
		},
	}
	state := models.BlockNodeState{}
	mockChecker := new(MockRealityChecker)

	err := InitBlockNodeRuntime(cfg, state, mockChecker, 5*time.Second)
	require.NoError(t, err)
	br := BlockNode()

	t.Run("Success", func(t *testing.T) {
		newStorage := models.BlockNodeStorage{
			LiveSize: "10Gi",
		}
		err := br.setStorageInput(newStorage)
		require.NoError(t, err)
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
			Storage: models.BlockNodeStorage{
				LiveSize: "5Gi",
			},
		},
	}
	state := models.BlockNodeState{
		ReleaseInfo: models.HelmReleaseInfo{
			Name:         "test-release",
			Namespace:    "test-namespace",
			Version:      "v1.0.0",
			ChartName:    "block-node",
			ChartRef:     "repo/chart",
			ChartVersion: "1.0.0",
			Status:       release.StatusDeployed,
		},
	}
	mockChecker := new(MockRealityChecker)

	err := InitBlockNodeRuntime(cfg, state, mockChecker, 5*time.Second)
	require.NoError(t, err)
	br := BlockNode()

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

func TestBlockNodeRuntime_Singleton(t *testing.T) {
	cfg := models.Config{}
	state := models.BlockNodeState{}
	mockChecker := new(MockRealityChecker)

	err := InitBlockNodeRuntime(cfg, state, mockChecker, 5*time.Second)
	require.NoError(t, err)

	runtime1 := BlockNode()
	runtime2 := BlockNode()

	assert.Same(t, runtime1, runtime2, "BlockNode() should return the same singleton instance")
}
