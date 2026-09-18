// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package node

import (
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCheckResolver builds a not-deployed block-node resolver seeded only from
// cfg, the shape `check` sees on a machine that has never run `install`.
func newCheckResolver(t *testing.T, cfg models.Config) *rsl.BlockNodeRuntimeResolver {
	t.Helper()
	r, err := rsl.NewBlockNodeRuntimeResolver(cfg, state.NewBlockNodeState(), nil, time.Minute)
	require.NoError(t, err)
	return r.(*rsl.BlockNodeRuntimeResolver)
}

func checkConfig() models.Config {
	return models.Config{BlockNode: models.BlockNodeConfig{
		ChartVersion: "0.37.0",
		Storage:      models.BlockNodeStorage{BasePath: "/mnt/config"},
	}}
}

func TestResolveStorageForCheck_FlagsWinOverConfig(t *testing.T) {
	storage, chartVersion := resolveStorageForCheck(newCheckResolver(t, checkConfig()),
		models.BlockNodeStorage{BasePath: "/mnt/flags"})

	assert.Equal(t, "/mnt/flags", storage.BasePath, "the operator's flags name the paths install would use")
	assert.Equal(t, "0.37.0", chartVersion)
}

func TestResolveStorageForCheck_ConfigWhenNoFlags(t *testing.T) {
	storage, chartVersion := resolveStorageForCheck(newCheckResolver(t, checkConfig()), models.BlockNodeStorage{})

	assert.Equal(t, "/mnt/config", storage.BasePath)
	assert.Equal(t, "0.37.0", chartVersion)
}

func TestResolveStorageForCheck_UnresolvableChartVersionDegradesToZero(t *testing.T) {
	// No config, no flags, no defaults: nothing can name a chart version, and
	// the warn-only check must degrade rather than fail `check`.
	storage, chartVersion := resolveStorageForCheck(newCheckResolver(t, models.Config{}),
		models.BlockNodeStorage{BasePath: "/mnt/flags"})

	assert.Equal(t, models.BlockNodeStorage{}, storage)
	assert.Empty(t, chartVersion)
}
