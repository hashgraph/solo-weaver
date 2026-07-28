// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/migration"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/chartutil"
)

// writeValuesFile writes content to a temp values file and returns its path.
func writeValuesFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "values.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// ── EffectivePluginsNamesEmpty ─────────────────────────────────────────────────

func TestEffectivePluginsNamesEmpty(t *testing.T) {
	tests := []struct {
		name       string
		pluginList string
		fileBody   string // empty => no --values file
		want       bool
	}{
		{"resolved list wins over empty values", "health,verification", "plugins:\n  names: \"\"\n", false},
		{"values sets explicit empty string", "", "plugins:\n  names: \"\"\n", true},
		{"values sets explicit null", "", "plugins:\n  names: null\n", true},
		{"values sets empty sequence", "", "plugins:\n  names: []\n", true},
		{"values sets a concrete list", "", "plugins:\n  names: \"health\"\n", false},
		{"values does not define names", "", "plugins:\n  mavenImage: img\n", false},
		{"no list and no values file", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			valuesFile := ""
			if tc.fileBody != "" {
				valuesFile = writeValuesFile(t, tc.fileBody)
			}
			assert.Equal(t, tc.want, EffectivePluginsNamesEmpty(tc.pluginList, valuesFile))
		})
	}
}

// ── injectPersistenceOverrides: baked image omits the plugins claim ────────────

func TestInjectPersistenceOverrides_PluginsBakedSkipsPluginsClaim(t *testing.T) {
	valuesFile := writeValuesFile(t, "plugins:\n  names: \"\"\n")
	m := &Manager{
		// 0.28.1 makes both verification and plugins applicable; the baked signal
		// must drop only plugins, leaving verification managed.
		blockNodeInputs: models.BlockNodeInputs{ChartVersion: "0.28.1", ValuesFile: valuesFile},
		logger:          testLogger(),
	}

	out, err := m.injectPersistenceOverrides([]byte("blockNode:\n  persistence: {}\n"))
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(out, &vals))
	persistence := vals["blockNode"].(map[string]interface{})["persistence"].(map[string]interface{})

	_, hasPlugins := persistence["plugins"]
	assert.False(t, hasPlugins, "baked image must not force a plugins existingClaim")
	_, hasVerification := persistence["verification"]
	assert.True(t, hasVerification, "verification storage must still be managed")
}

func TestInjectPersistenceOverrides_ManagedKeepsPluginsClaim(t *testing.T) {
	// No --values file and no resolved empty → plugins stays managed (today's behavior).
	m := &Manager{
		blockNodeInputs: models.BlockNodeInputs{ChartVersion: "0.28.1"},
		logger:          testLogger(),
	}

	out, err := m.injectPersistenceOverrides([]byte("blockNode:\n  persistence: {}\n"))
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(out, &vals))
	persistence := vals["blockNode"].(map[string]interface{})["persistence"].(map[string]interface{})

	plugins, ok := persistence["plugins"].(map[string]interface{})
	require.True(t, ok, "plugins storage must be managed when not a baked image")
	assert.Equal(t, "plugins-storage-pvc", plugins["existingClaim"])
	assert.Equal(t, false, plugins["create"])
}

// ── injectPluginsConfig: baked image forces an explicit empty plugins.names ────

func TestInjectPluginsConfig_BakedForcesEmptyNames(t *testing.T) {
	valuesFile := writeValuesFile(t, "plugins:\n  names: null\n")
	m := &Manager{
		blockNodeInputs: models.BlockNodeInputs{ChartVersion: "0.28.1", ValuesFile: valuesFile},
		logger:          testLogger(),
	}

	// Base carries a non-empty default (as weaver's base template does).
	out, err := m.injectPluginsConfig([]byte("plugins:\n  names: \"health,verification\"\n"))
	require.NoError(t, err)

	var vals map[string]interface{}
	require.NoError(t, yaml.Unmarshal(out, &vals))
	names, ok := vals["plugins"].(map[string]interface{})["names"]
	require.True(t, ok, "plugins.names must be present (explicitly empty), not absent")
	assert.Equal(t, "", names, "baked image must force plugins.names to an explicit empty string")
}

func TestInjectPluginsConfig_NoOverrideLeavesDefault(t *testing.T) {
	// No resolved list, no values file → leave the base/chart default untouched.
	m := &Manager{
		blockNodeInputs: models.BlockNodeInputs{ChartVersion: "0.28.1"},
		logger:          testLogger(),
	}
	in := []byte("plugins:\n  names: \"health,verification\"\n")
	out, err := m.injectPluginsConfig(in)
	require.NoError(t, err)
	assert.Equal(t, string(in), string(out), "default list must be left intact")
}

// ── Two-layer merge: prove the CHART ends up seeing an empty plugins.names ──────
//
// This is the load-bearing invariant for #913: removing weaver's forced
// existingClaim is only safe if the chart's own gate (plugins.names OR
// existingClaim) is also false. Helm re-applies the chart's own non-empty
// values.yaml plugins.names default unless weaver's -f output carries an explicit
// empty. This test runs weaver's real pipeline (renderDefaultValues →
// mergeValues(operator) → injectPluginsConfig) and then simulates Helm coalescing
// weaver's output over the chart's non-empty default.
func TestPluginsBakedTwoLayerMerge_ChartSeesEmpty(t *testing.T) {
	// Stand-in for the chart's values.yaml default (a non-empty plugin list).
	chartDefault := func() map[string]interface{} {
		return map[string]interface{}{
			"plugins": map[string]interface{}{
				"names": "backfill,facility-messaging,health,server-status",
			},
		}
	}

	// pluginsNames extracts the effective plugins.names Helm would render with.
	effectiveNames := func(t *testing.T, m *Manager, operatorBody string) interface{} {
		t.Helper()
		base, err := m.renderDefaultValues(models.ProfileLocal)
		require.NoError(t, err)
		merged, err := mergeValues(base, []byte(operatorBody))
		require.NoError(t, err)
		final, err := m.injectPluginsConfig(merged)
		require.NoError(t, err)

		var weaverVals map[string]interface{}
		require.NoError(t, yaml.Unmarshal(final, &weaverVals))
		// Helm: user-supplied -f values (dst) coalesced over chart defaults (src).
		coalesced := chartutil.CoalesceTables(weaverVals, chartDefault())
		return coalesced["plugins"].(map[string]interface{})["names"]
	}

	t.Run("baked image: operator empties names → chart sees empty", func(t *testing.T) {
		operatorBody := "plugins:\n  names: \"\"\n"
		valuesFile := writeValuesFile(t, operatorBody)
		m := &Manager{
			blockNodeInputs: models.BlockNodeInputs{ChartVersion: "0.28.1", ValuesFile: valuesFile},
			logger:          testLogger(),
		}
		names := effectiveNames(t, m, operatorBody)
		assert.Equal(t, "", names, "chart must render with an empty plugins.names (no volume, baked plugins used)")
	})

	t.Run("default: no override → chart sees weaver base default (non-empty)", func(t *testing.T) {
		m := &Manager{
			blockNodeInputs: models.BlockNodeInputs{ChartVersion: "0.28.1"},
			logger:          testLogger(),
		}
		names := effectiveNames(t, m, "blockNode:\n  config: {}\n")
		assert.NotEmpty(t, names, "chart must keep a non-empty plugins.names (managed/maven mode)")
	})
}

// ── plugins StorageMigration.Applies honors the baked signal ───────────────────

func TestPluginsStorageMigration_Applies_BakedSignal(t *testing.T) {
	m := NewPluginsStorageMigration()

	// An upgrade that crosses the plugins boundary and would otherwise apply.
	newCtx := func(managePluginsSet, managePlugins bool) *migration.Context {
		ctx := &migration.Context{Data: &automa.SyncStateBag{}}
		ctx.Data.Set(migration.CtxKeyInstalledVersion, "0.27.0")
		ctx.Data.Set(migration.CtxKeyTargetVersion, "0.28.1")
		if managePluginsSet {
			ctx.Data.Set(ctxKeyManagePlugins, managePlugins)
		}
		return ctx
	}

	t.Run("baked image (managePlugins=false) → does not apply", func(t *testing.T) {
		applies, err := m.Applies(newCtx(true, false))
		require.NoError(t, err)
		assert.False(t, applies)
	})

	t.Run("managed (managePlugins=true) → applies", func(t *testing.T) {
		applies, err := m.Applies(newCtx(true, true))
		require.NoError(t, err)
		assert.True(t, applies)
	})

	t.Run("signal absent → applies (pre-#913 default)", func(t *testing.T) {
		applies, err := m.Applies(newCtx(false, false))
		require.NoError(t, err)
		assert.True(t, applies)
	})
}
