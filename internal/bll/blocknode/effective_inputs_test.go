// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

// fakeBlockNodeChecker is a no-op reality.Checker[state.BlockNodeState] that
// returns a fixed (not-deployed) state, so effective-input resolution takes the
// user-supplied values.
type fakeBlockNodeChecker struct{ st state.BlockNodeState }

func (f *fakeBlockNodeChecker) RefreshState(_ context.Context) (state.BlockNodeState, error) {
	return f.st, nil
}

// TestResolveEffectiveInputs_CarriesTimeout is a regression guard for #912: the
// --timeout value must survive resolveBlocknodeEffectiveInputs, which rebuilds
// BlockNodeInputs field-by-field. A field left out of that literal is silently
// dropped between CLI validation and the workflow, so the helm call falls back
// to the 5m default (which is exactly what happened before this fix).
func TestResolveEffectiveInputs_CarriesTimeout(t *testing.T) {
	checker := &fakeBlockNodeChecker{st: state.NewBlockNodeState()}
	r, err := rsl.NewBlockNodeRuntimeResolver(models.Config{}, state.NewBlockNodeState(), checker, 10*time.Minute)
	require.NoError(t, err)
	runtime := r.(*rsl.BlockNodeRuntimeResolver)

	inputs := models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:    "block-node",
			Release:      "block-node",
			Chart:        "oci://example.com/block-node",
			ChartVersion: "0.37.1",
			Storage:      models.BlockNodeStorage{BasePath: "/mnt/fast-storage"},
			Timeout:      15 * time.Minute,
		},
	}

	eff, err := resolveBlocknodeEffectiveInputs(
		runtime,
		models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode},
		inputs,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, 15*time.Minute, eff.Custom.Timeout,
		"--timeout must be carried through effective-input resolution")
}

// TestResolveEffectiveInputs_CarriesStatusz is a regression guard (same class as
// the #912 timeout drop): the operator-supplied statusz overrides must survive
// resolveBlocknodeEffectiveInputs, which rebuilds BlockNodeInputs field-by-field.
// A field omitted from that literal is silently dropped between CLI validation and
// the daemon-config step, so daemon.yaml would never receive the operator's value.
func TestResolveEffectiveInputs_CarriesStatusz(t *testing.T) {
	checker := &fakeBlockNodeChecker{st: state.NewBlockNodeState()}
	r, err := rsl.NewBlockNodeRuntimeResolver(models.Config{}, state.NewBlockNodeState(), checker, 10*time.Minute)
	require.NoError(t, err)
	runtime := r.(*rsl.BlockNodeRuntimeResolver)

	inputs := models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:           "block-node",
			Release:             "block-node",
			Chart:               "oci://example.com/block-node",
			ChartVersion:        "0.37.1",
			Storage:             models.BlockNodeStorage{BasePath: "/mnt/fast-storage"},
			StatuszBaseURL:      "http://127.0.0.1:8080",
			StatuszPollInterval: "5s",
		},
	}

	eff, err := resolveBlocknodeEffectiveInputs(
		runtime,
		models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode},
		inputs,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:8080", eff.Custom.StatuszBaseURL,
		"--statusz-base-url must be carried through effective-input resolution")
	assert.Equal(t, "5s", eff.Custom.StatuszPollInterval,
		"--statusz-poll-interval must be carried through effective-input resolution")
}

// baseTcInputs returns valid inputs with the resolvable fields set but the
// traffic-shaping content deliberately left empty, so the state fallback governs.
func baseTcInputs() models.UserInputs[models.BlockNodeInputs] {
	return models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{
			Namespace:    "block-node",
			Release:      "block-node",
			Chart:        "oci://example.com/block-node",
			ChartVersion: "0.37.1",
			Storage:      models.BlockNodeStorage{BasePath: "/mnt/fast-storage"},
		},
	}
}

// deployedShapingBlockNodeState returns a deployed BlockNodeState so that
// resolveBlocknodeEffectiveInputs can resolve an upgrade (chart-version
// resolution requires a StrategyState/Reality source, i.e. a deployed release).
// BasePath is set so the storage resolver's deployed-state validation passes.
func deployedShapingBlockNodeState() state.BlockNodeState {
	st := state.NewBlockNodeState()
	st.ReleaseInfo = state.HelmReleaseInfo{
		Status:       release.StatusDeployed,
		Name:         "block-node",
		Namespace:    "block-node",
		ChartName:    "block-node",
		ChartRef:     "oci://example.com/block-node",
		ChartVersion: "0.37.1",
	}
	st.Storage = models.BlockNodeStorage{BasePath: "/mnt/fast-storage"}
	return st
}

// TestResolveEffectiveInputs_TrafficShapingFallbackFromState verifies that when
// the operator supplies no egress NIC / rate (the upgrade case, which has no such
// flags), the persisted BlockNodeState.Shaping is used so the tc steps re-assert
// the original egress device and trunk rate instead of auto-detecting
// (issue #932, AC3).
//
// ShapeOverrides is deliberately NOT backfilled: re-asserting an install-time
// --shape would overwrite a later `network shape set` on the same class, which is
// the clobber #1037 fixes. Per-class values live in the shape registry, which the
// tc steps now preserve across a re-provision at an unchanged trunk rate.
func TestResolveEffectiveInputs_TrafficShapingFallbackFromState(t *testing.T) {
	prio := 0
	persisted := deployedShapingBlockNodeState()
	persisted.Shaping = &state.ShapingState{
		EgressInterface: "eth0",
		LinkRate:        "1gbit",
		ShapeOverrides: map[string]models.ShapeOverride{
			"publisher": {Rate: "800mbit", Ceil: "1gbit", Prio: &prio},
		},
	}

	checker := &fakeBlockNodeChecker{st: state.NewBlockNodeState()}
	r, err := rsl.NewBlockNodeRuntimeResolver(models.Config{}, persisted, checker, 10*time.Minute)
	require.NoError(t, err)
	runtime := r.(*rsl.BlockNodeRuntimeResolver)

	eff, err := resolveBlocknodeEffectiveInputs(
		runtime,
		models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlockNode},
		baseTcInputs(),
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "eth0", eff.Custom.EgressInterface, "egress NIC must fall back to persisted Shaping")
	assert.Equal(t, "1gbit", eff.Custom.LinkRate, "link rate must fall back to persisted Shaping")
	assert.Empty(t, eff.Custom.ShapeOverrides,
		"persisted --shape must NOT be re-asserted; it would clobber later `network shape set` tuning (#1037)")
}

// TestResolveEffectiveInputs_ExplicitTrafficShapingWins verifies that an explicit
// operator-supplied value beats the persisted state fallback (issue #932, AC4).
func TestResolveEffectiveInputs_ExplicitTrafficShapingWins(t *testing.T) {
	persisted := state.NewBlockNodeState()
	persisted.Shaping = &state.ShapingState{EgressInterface: "eth0", LinkRate: "1gbit"}

	checker := &fakeBlockNodeChecker{st: state.NewBlockNodeState()}
	r, err := rsl.NewBlockNodeRuntimeResolver(models.Config{}, persisted, checker, 10*time.Minute)
	require.NoError(t, err)
	runtime := r.(*rsl.BlockNodeRuntimeResolver)

	inputs := baseTcInputs()
	inputs.Custom.EgressInterface = "ens5"
	inputs.Custom.LinkRate = "10gbit"
	inputs.Custom.ShapeOverrides = map[string]models.ShapeOverride{
		"partner": {Rate: "300mbit"},
	}

	eff, err := resolveBlocknodeEffectiveInputs(
		runtime,
		models.Intent{Action: models.ActionReconfigure, Target: models.TargetBlockNode},
		inputs,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "ens5", eff.Custom.EgressInterface, "explicit egress NIC must win over persisted Shaping")
	assert.Equal(t, "10gbit", eff.Custom.LinkRate, "explicit link rate must win over persisted Shaping")
	require.Contains(t, eff.Custom.ShapeOverrides, "partner",
		"--shape supplied on this run must reach the tc steps")
	assert.Equal(t, "300mbit", eff.Custom.ShapeOverrides["partner"].Rate)
}

// withConfigFile writes body to a temp config.yaml and loads it through
// config.Initialize, so config.File() reports a real path for the duration of the
// test. The previous global config is restored on cleanup: pkg/config keeps it in
// a package var, so a leaked config file would bleed into sibling tests.
func withConfigFile(t *testing.T, body string) {
	t.Helper()

	saved := config.Get()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	require.NoError(t, config.Initialize(path))

	t.Cleanup(func() {
		require.NoError(t, config.Initialize(""))
		require.NoError(t, config.Set(&saved))
	})
}

// newDeployedRuntime builds a resolver over a deployed release, wired the way
// cmd/cli/commands/block/node/init.go wires it: config from the file (or the
// compiled-in defaults when none was loaded), then defaults, then env.
//
// The reality checker returns the same deployed state, so if a selector is ever
// changed to consult StrategyReality where it consults StrategyState today, these
// tests keep describing a deployed release rather than quietly falling through to
// the not-deployed branch. resolveBlocknodeEffectiveInputs does not call
// RefreshState itself — the handler does, before it — so nothing here populates
// StrategyReality; this only keeps the fixture honest about what it models.
func newDeployedRuntime(t *testing.T, st state.BlockNodeState) *rsl.BlockNodeRuntimeResolver {
	t.Helper()

	checker := &fakeBlockNodeChecker{st: st}
	r, err := rsl.NewBlockNodeRuntimeResolver(config.Get(), st, checker, 10*time.Minute)
	require.NoError(t, err)

	runtime := r.(*rsl.BlockNodeRuntimeResolver)
	runtime.WithDefaults(config.DefaultsConfig())
	runtime.WithEnv(config.EnvConfig())
	return runtime
}

// TestResolveEffectiveInputs_ExplicitConfigBeatsDeployedState pins the rule that
// an operator-supplied --config file outranks the deployed release. The RSL
// selectors lock chart ref, chart version and storage to that release, so without
// this promotion a file declaring different values is silently dropped and the
// release is re-applied with whatever state.yaml recorded.
func TestResolveEffectiveInputs_ExplicitConfigBeatsDeployedState(t *testing.T) {
	withConfigFile(t, `
blockNode:
  chart: oci://registry.example.com/hiero/block-node
  version: 0.40.0
  storage:
    liveSize: 500Gi
`)

	runtime := newDeployedRuntime(t, deployedShapingBlockNodeState())

	eff, err := resolveBlocknodeEffectiveInputs(
		runtime,
		models.Intent{Action: models.ActionReconfigure, Target: models.TargetBlockNode},
		models.UserInputs[models.BlockNodeInputs]{},
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "oci://registry.example.com/hiero/block-node", eff.Custom.Chart,
		"chart declared in an explicit --config file must beat the deployed release")
	assert.Equal(t, "0.40.0", eff.Custom.ChartVersion,
		"chart version declared in an explicit --config file must beat the deployed release")
	assert.Equal(t, "500Gi", eff.Custom.Storage.LiveSize,
		"storage size declared in an explicit --config file must beat the deployed release")
	assert.Equal(t, "/mnt/fast-storage", eff.Custom.Storage.BasePath,
		"fields the file leaves out must still come from the deployed release")
}

// TestResolveEffectiveInputs_ExplicitConfigBasePathReplacesDeployedPaths pins the
// base-path half of the storage merge. The reality checker reads the individual
// hostPaths back off the PVs, so merging the deployed storage in after a
// file-supplied BasePath would leave both set and the old paths would win at
// render time. MergeFrom's base-path mode is what prevents that.
func TestResolveEffectiveInputs_ExplicitConfigBasePathReplacesDeployedPaths(t *testing.T) {
	withConfigFile(t, `
blockNode:
  storage:
    basePath: /mnt/new-storage
`)

	st := deployedShapingBlockNodeState()
	st.Storage = models.BlockNodeStorage{
		ArchivePath: "/mnt/fast-storage/archive",
		LivePath:    "/mnt/fast-storage/live",
		LogPath:     "/mnt/fast-storage/logs",
	}

	eff, err := resolveBlocknodeEffectiveInputs(
		newDeployedRuntime(t, st),
		models.Intent{Action: models.ActionReconfigure, Target: models.TargetBlockNode},
		models.UserInputs[models.BlockNodeInputs]{},
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "/mnt/new-storage", eff.Custom.Storage.BasePath)
	assert.Empty(t, eff.Custom.Storage.ArchivePath,
		"deployed hostPaths must not be merged back in on top of a file-supplied basePath")
	assert.Empty(t, eff.Custom.Storage.LivePath)
	assert.Empty(t, eff.Custom.Storage.LogPath)
}

// TestResolveEffectiveInputs_FlagBeatsExplicitConfig verifies the promotion does
// not overshoot: --chart-version still outranks the file it was passed alongside.
func TestResolveEffectiveInputs_FlagBeatsExplicitConfig(t *testing.T) {
	withConfigFile(t, `
blockNode:
  version: 0.40.0
`)

	inputs := models.UserInputs[models.BlockNodeInputs]{
		Custom: models.BlockNodeInputs{ChartVersion: "0.41.0"},
	}

	eff, err := resolveBlocknodeEffectiveInputs(
		newDeployedRuntime(t, deployedShapingBlockNodeState()),
		models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlockNode},
		inputs,
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "0.41.0", eff.Custom.ChartVersion,
		"--chart-version must outrank the --config file")
}

// TestResolveEffectiveInputs_EnvBeatsExplicitConfig pins the middle rung of the
// order: SOLO_PROVISIONER_* outranks the file, and both outrank the deployed
// release. Returning the resolver's answer here instead would hand back the
// deployed value — a third outcome matching neither thing the operator set.
func TestResolveEffectiveInputs_EnvBeatsExplicitConfig(t *testing.T) {
	t.Setenv("SOLO_PROVISIONER_BLOCKNODE_CHART", "oci://env.example.com/block-node")
	withConfigFile(t, `
blockNode:
  chart: oci://file.example.com/block-node
`)

	eff, err := resolveBlocknodeEffectiveInputs(
		newDeployedRuntime(t, deployedShapingBlockNodeState()),
		models.Intent{Action: models.ActionReconfigure, Target: models.TargetBlockNode},
		models.UserInputs[models.BlockNodeInputs]{},
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "oci://env.example.com/block-node", eff.Custom.Chart,
		"SOLO_PROVISIONER_* must outrank both the --config file and the deployed release")
}

// TestResolveEffectiveInputs_UninstallIgnoresExplicitConfig pins the scope
// boundary of the promotion. uninstall clears the directories the effective
// storage names and deletes the PVs without going through planStorage, so
// honouring a file here would point the wipe at a tree the block node never
// used: the live data would survive and whatever did live at the declared path
// would not.
func TestResolveEffectiveInputs_UninstallIgnoresExplicitConfig(t *testing.T) {
	withConfigFile(t, `
blockNode:
  storage:
    basePath: /srv/elsewhere
`)

	eff, err := resolveBlocknodeEffectiveInputs(
		newDeployedRuntime(t, deployedShapingBlockNodeState()),
		models.Intent{Action: models.ActionUninstall, Target: models.TargetBlockNode},
		models.UserInputs[models.BlockNodeInputs]{},
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "/mnt/fast-storage", eff.Custom.Storage.BasePath,
		"uninstall must wipe the deployed paths, never a path the config file names")
}

// TestResolveEffectiveInputs_ResetHonoursExplicitConfig is the other side of that
// boundary. reset goes through planStorage, which purges at the deployed paths
// and refuses a path change without --purge-storage, so a file is free to move
// storage the one way --purge-storage is there to allow.
func TestResolveEffectiveInputs_ResetHonoursExplicitConfig(t *testing.T) {
	withConfigFile(t, `
blockNode:
  storage:
    basePath: /mnt/new-storage
`)

	eff, err := resolveBlocknodeEffectiveInputs(
		newDeployedRuntime(t, deployedShapingBlockNodeState()),
		models.Intent{Action: models.ActionReset, Target: models.TargetBlockNode},
		models.UserInputs[models.BlockNodeInputs]{},
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "/mnt/new-storage", eff.Custom.Storage.BasePath,
		"reset --purge-storage must be able to recreate storage where the config file says")
}

// TestResolveEffectiveInputs_ExplicitConfigPromotesEveryStorageField covers the
// storage fields BlockNodeStorage.IsEmpty() does not look at. Gating the cascade
// on that predicate dropped a file declaring only one of them, which is the
// silent-no-op this change exists to remove.
func TestResolveEffectiveInputs_ExplicitConfigPromotesEveryStorageField(t *testing.T) {
	withConfigFile(t, `
blockNode:
  storage:
    pluginsSize: 20Gi
`)

	st := deployedShapingBlockNodeState()
	st.Storage.PluginsSize = "5Gi"

	eff, err := resolveBlocknodeEffectiveInputs(
		newDeployedRuntime(t, st),
		models.Intent{Action: models.ActionReconfigure, Target: models.TargetBlockNode},
		models.UserInputs[models.BlockNodeInputs]{},
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "20Gi", eff.Custom.Storage.PluginsSize,
		"a field outside IsEmpty()'s seven must still be promoted over the deployed release")
}

// TestResolveEffectiveInputs_IdentityFieldsStayLockedToDeployedRelease guards the
// scope boundary of the config promotion. Namespace, release name and chart name
// are not reconfigurable: pointing them somewhere else makes Helm address a second
// release and orphan the running one, so a config file must not be able to.
func TestResolveEffectiveInputs_IdentityFieldsStayLockedToDeployedRelease(t *testing.T) {
	withConfigFile(t, `
blockNode:
  namespace: other-ns
  release: other-release
  chartName: other-chart
`)

	eff, err := resolveBlocknodeEffectiveInputs(
		newDeployedRuntime(t, deployedShapingBlockNodeState()),
		models.Intent{Action: models.ActionReconfigure, Target: models.TargetBlockNode},
		models.UserInputs[models.BlockNodeInputs]{},
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "block-node", eff.Custom.Namespace)
	assert.Equal(t, "block-node", eff.Custom.Release)
	assert.Equal(t, "block-node", eff.Custom.ChartName)
}

// TestResolveEffectiveInputs_WithoutConfigFileDeployedStateStillWins is the
// regression guard that makes the whole change safe. pkg/config pre-populates
// globalConfig with the deps constants, so the config tier is non-empty even with
// no --config; promoting it unconditionally would reset a deployed release to the
// compiled-in chart version on every invocation.
func TestResolveEffectiveInputs_WithoutConfigFileDeployedStateStillWins(t *testing.T) {
	require.Empty(t, config.File(), "this test must run with no --config loaded")

	st := deployedShapingBlockNodeState()
	st.ReleaseInfo.ChartVersion = "0.37.1"
	st.ReleaseInfo.ChartRef = "oci://example.com/block-node"

	eff, err := resolveBlocknodeEffectiveInputs(
		newDeployedRuntime(t, st),
		models.Intent{Action: models.ActionReconfigure, Target: models.TargetBlockNode},
		models.UserInputs[models.BlockNodeInputs]{},
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "0.37.1", eff.Custom.ChartVersion)
	assert.Equal(t, "oci://example.com/block-node", eff.Custom.Chart)
	assert.Equal(t, "/mnt/fast-storage", eff.Custom.Storage.BasePath)
}

// TestResolveEffectiveInputs_ConfigFileOmissionsFallBackToDeployedState pins the
// other half of the promotion contract: --config promotes only the fields the
// file actually declares. A key the file leaves out must still resolve to the
// deployed release, not to a compiled-in default.
//
// This is not incidental. config.Initialize zeroes globalConfig before
// unmarshalling, so an omitted key arrives as "" — which is exactly what
// preferConfigFile treats as "the file said nothing, keep the resolver's answer".
// Were that guard ever dropped, passing a partial config file would silently
// reset every field it did not mention.
func TestResolveEffectiveInputs_ConfigFileOmissionsFallBackToDeployedState(t *testing.T) {
	// Declares storage sizing only: no chart, no version, no base path.
	withConfigFile(t, `
blockNode:
  storage:
    liveSize: 500Gi
`)

	eff, err := resolveBlocknodeEffectiveInputs(
		newDeployedRuntime(t, deployedShapingBlockNodeState()),
		models.Intent{Action: models.ActionReconfigure, Target: models.TargetBlockNode},
		models.UserInputs[models.BlockNodeInputs]{},
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "oci://example.com/block-node", eff.Custom.Chart,
		"a chart the config file does not declare must stay at the deployed value")
	assert.Equal(t, "0.37.1", eff.Custom.ChartVersion,
		"a version the config file does not declare must stay at the deployed value")
	assert.Equal(t, "/mnt/fast-storage", eff.Custom.Storage.BasePath,
		"a storage field the config file does not declare must stay at the deployed value")
	assert.Equal(t, "500Gi", eff.Custom.Storage.LiveSize,
		"the one field the file did declare must still be promoted")
}
