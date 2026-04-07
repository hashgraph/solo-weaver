// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package rsl

import (
	"context"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
)

// ── Test doubles ──────────────────────────────────────────────────────────────

// mockBlockNodeChecker is a controllable reality.Checker[state.BlockNodeState].
type mockBlockNodeChecker struct {
	state state.BlockNodeState
	err   error
	calls int
}

func (m *mockBlockNodeChecker) RefreshState(_ context.Context) (state.BlockNodeState, error) {
	m.calls++
	return m.state, m.err
}

// ── Fixtures ──────────────────────────────────────────────────────────────────

// deployedState returns a fully-populated BlockNodeState with StatusDeployed.
func deployedState() state.BlockNodeState {
	return state.BlockNodeState{
		ReleaseInfo: state.HelmReleaseInfo{
			Name:         "block-node",
			Namespace:    "block-node-ns",
			ChartVersion: "0.29.0",
			ChartName:    "block-node-server",
			ChartRef:     "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server",
			Status:       release.StatusDeployed,
		},
		Storage: models.BlockNodeStorage{
			BasePath: "/mnt/fast-storage",
			LiveSize: "10Gi",
		},
	}
}

// fullConfig returns a models.Config with every BlockNode field populated.
func fullConfig() models.Config {
	return models.Config{
		BlockNode: models.BlockNodeConfig{
			Namespace:    "config-ns",
			Release:      "config-release",
			ChartName:    "config-chart-name",
			Chart:        "oci://config/chart",
			ChartVersion: "1.0.0",
			Storage: models.BlockNodeStorage{
				BasePath: "/mnt/config-storage",
			},
		},
	}
}

// testDefaultsConfig mimics the deps-based defaults used by config.DefaultsConfig().
func testDefaultsConfig() models.Config {
	return models.Config{
		BlockNode: models.BlockNodeConfig{
			Namespace:    "block-node",
			Release:      "block-node",
			Chart:        "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server",
			ChartVersion: "0.29.0",
			Storage: models.BlockNodeStorage{
				BasePath: "/mnt/fast-storage",
			},
		},
	}
}

// installIntent is the default non-upgrade intent used by most tests.
var installIntent = models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}

// newTestResolver creates a BlockNodeRuntimeResolver backed by a no-op mock checker
// with a 10-minute refresh interval.  The concrete type is returned so callers can
// invoke field-accessor methods (Namespace, ReleaseName, etc.) directly.
func newTestResolver(cfg models.Config, blockNodeState state.BlockNodeState) *BlockNodeRuntimeResolver {
	checker := &mockBlockNodeChecker{state: blockNodeState}
	r, err := NewBlockNodeRuntimeResolver(cfg, blockNodeState, checker, 10*time.Minute)
	if err != nil {
		panic("newTestResolver: " + err.Error())
	}
	return r.(*BlockNodeRuntimeResolver)
}

// ── Constructor ───────────────────────────────────────────────────────────────

func TestNewBlockNodeRuntimeResolver_SeedsConfigFromConstructor(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())

	ns, err := r.Namespace()
	require.NoError(t, err)

	cfgVal, _ := ns.ConfigVal()
	assert.Equal(t, "config-ns", cfgVal.Val(), "constructor must seed StrategyConfig")
}

func TestNewBlockNodeRuntimeResolver_NotDeployed_NoStateSources(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())

	ns, err := r.Namespace()
	require.NoError(t, err)

	stateVal, _ := ns.StateVal()
	assert.Equal(t, "", stateVal.Val(), "non-deployed state must not register StrategyState")
}

func TestNewBlockNodeRuntimeResolver_DeployedState_SeedsStateSources(t *testing.T) {
	r := newTestResolver(fullConfig(), deployedState())

	ns, err := r.Namespace()
	require.NoError(t, err)

	stateVal, _ := ns.StateVal()
	assert.Equal(t, "block-node-ns", stateVal.Val(), "deployed constructor state must seed StrategyState")
}

// ── WithDefaults ──────────────────────────────────────────────────────────────

func TestBlockNodeRuntimeResolver_WithDefaults_SeedsStrategyDefault(t *testing.T) {
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())

	ns, err := r.Namespace()
	require.NoError(t, err)

	defVal, _ := ns.DefaultVal()
	assert.Equal(t, "block-node", defVal.Val())
}

func TestBlockNodeRuntimeResolver_WithDefaults_AllFields(t *testing.T) {
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())

	// release name
	rel, err := r.ReleaseName()
	require.NoError(t, err)
	dv, _ := rel.DefaultVal()
	assert.Equal(t, "block-node", dv.Val())

	// chart ref
	cr, err := r.ChartRef()
	require.NoError(t, err)
	dv2, _ := cr.DefaultVal()
	assert.Equal(t, "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server", dv2.Val())

	// chart version (needs intent)
	r.WithIntent(installIntent)
	cv, err := r.ChartVersion()
	require.NoError(t, err)
	dv3, _ := cv.DefaultVal()
	assert.Equal(t, "0.29.0", dv3.Val())

	// storage default BasePath
	st, err := r.Storage()
	require.NoError(t, err)
	defStorage, _ := st.DefaultVal()
	assert.Equal(t, "/mnt/fast-storage", defStorage.Val().BasePath)
}

func TestBlockNodeRuntimeResolver_WithDefaults_EmptyFieldsAreNotRegistered(t *testing.T) {
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())

	// Defaults config with no ChartName
	defCfg := testDefaultsConfig()
	defCfg.BlockNode.ChartName = ""
	r.WithDefaults(defCfg)

	cn, err := r.ChartName()
	require.NoError(t, err)

	defVal, _ := cn.DefaultVal()
	assert.Equal(t, "", defVal.Val(), "empty field must not be registered as StrategyDefault")
}

// ── WithConfig ────────────────────────────────────────────────────────────────

func TestBlockNodeRuntimeResolver_WithConfig_UpdatesStrategyConfig(t *testing.T) {
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithConfig(fullConfig())

	ns, err := r.Namespace()
	require.NoError(t, err)

	cfgVal, _ := ns.ConfigVal()
	assert.Equal(t, "config-ns", cfgVal.Val())

	// Config beats default
	assert.Equal(t, "config-ns", ns.Get().Val())
	assert.Equal(t, StrategyConfig, ns.Strategy())
}

// ── WithEnv ───────────────────────────────────────────────────────────────────

func TestBlockNodeRuntimeResolver_WithEnv_WinsOverConfig(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())

	r.WithEnv(models.Config{BlockNode: models.BlockNodeConfig{Namespace: "env-ns"}})

	ns, err := r.Namespace()
	require.NoError(t, err)
	assert.Equal(t, "env-ns", ns.Get().Val())
	assert.Equal(t, StrategyEnv, ns.Strategy())
}

func TestBlockNodeRuntimeResolver_WithEnv_EmptyValueNotRegistered(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())

	// Env config without namespace
	r.WithEnv(models.Config{})

	ns, err := r.Namespace()
	require.NoError(t, err)

	envVal, _ := ns.EnvVal()
	assert.Equal(t, "", envVal.Val(), "empty env field must not be registered")

	// Config still wins
	assert.Equal(t, "config-ns", ns.Get().Val())
	assert.Equal(t, StrategyConfig, ns.Strategy())
}

func TestBlockNodeRuntimeResolver_WithEnv_ClearsWhenOverriddenWithEmpty(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())

	// Set then clear env
	r.WithEnv(models.Config{BlockNode: models.BlockNodeConfig{Namespace: "env-ns"}})
	ns, _ := r.Namespace()
	require.Equal(t, "env-ns", ns.Get().Val())

	r.WithEnv(models.Config{}) // empty → clears StrategyEnv
	ns, _ = r.Namespace()
	envVal, _ := ns.EnvVal()
	assert.Equal(t, "", envVal.Val())
	assert.Equal(t, "config-ns", ns.Get().Val(), "config must win after env is cleared")
}

// ── WithUserInputs ────────────────────────────────────────────────────────────

func TestBlockNodeRuntimeResolver_WithUserInputs_WinsOverEnvAndConfig(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)
	r.WithEnv(models.Config{BlockNode: models.BlockNodeConfig{Namespace: "env-ns"}})

	r.WithUserInputs(models.BlockNodeInputs{
		Namespace:    "user-ns",
		Release:      "user-release",
		Chart:        "oci://user/chart",
		ChartVersion: "2.0.0",
	})

	ns, err := r.Namespace()
	require.NoError(t, err)
	assert.Equal(t, "user-ns", ns.Get().Val())
	assert.Equal(t, StrategyUserInput, ns.Strategy())
}

func TestBlockNodeRuntimeResolver_WithUserInputs_EmptyFieldClearsSource(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)

	r.WithUserInputs(models.BlockNodeInputs{
		Namespace: "user-ns", Release: "rel", Chart: "oci://c", ChartVersion: "1.0.0",
	})
	ns, _ := r.Namespace()
	require.Equal(t, "user-ns", ns.Get().Val())

	// Provide inputs without namespace — StrategyUserInput for namespace should be cleared
	r.WithUserInputs(models.BlockNodeInputs{Release: "rel", Chart: "oci://c", ChartVersion: "1.0.0"})
	ns, _ = r.Namespace()

	userVal, _ := ns.UserInputVal()
	assert.Equal(t, "", userVal.Val(), "cleared user input must not appear as source")
	assert.Equal(t, "config-ns", ns.Get().Val(), "config must win after user input is cleared")
}

// ── WithState ─────────────────────────────────────────────────────────────────

func TestBlockNodeRuntimeResolver_WithState_Deployed_SeedsStrategyState(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())

	r.WithState(deployedState())

	ns, err := r.Namespace()
	require.NoError(t, err)
	assert.Equal(t, "block-node-ns", ns.Get().Val())
	assert.Equal(t, StrategyState, ns.Strategy())

	stateVal, _ := ns.StateVal()
	assert.Equal(t, "block-node-ns", stateVal.Val())
}

func TestBlockNodeRuntimeResolver_WithState_NotDeployed_ClearsStateSources(t *testing.T) {
	// Start with a deployed state, then transition to not-deployed.
	r := newTestResolver(fullConfig(), deployedState())
	r.WithDefaults(testDefaultsConfig())

	r.WithState(state.NewBlockNodeState()) // StatusUnknown → not deployed

	ns, err := r.Namespace()
	require.NoError(t, err)

	stateVal, _ := ns.StateVal()
	assert.Equal(t, "", stateVal.Val(), "state source must be cleared when not deployed")
	// Falls back to config
	assert.Equal(t, "config-ns", ns.Get().Val())
}

// ── Full precedence cascade ───────────────────────────────────────────────────

// TestBlockNodeRuntimeResolver_PrecedenceCascade verifies that each layer correctly
// overrides all lower-priority layers by adding sources one at a time.
func TestBlockNodeRuntimeResolver_PrecedenceCascade_Namespace(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)

	// Layer 0: StrategyConfig (seeded by constructor via fullConfig)
	assertNamespace(t, r, "config-ns", StrategyConfig)

	// Layer 1: StrategyEnv beats StrategyConfig
	r.WithEnv(models.Config{BlockNode: models.BlockNodeConfig{Namespace: "env-ns"}})
	assertNamespace(t, r, "env-ns", StrategyEnv)

	// Layer 2: StrategyUserInput beats StrategyEnv
	r.WithUserInputs(models.BlockNodeInputs{
		Namespace: "user-ns", Release: "rel", Chart: "oci://chart", ChartVersion: "1.0.0",
	})
	assertNamespace(t, r, "user-ns", StrategyUserInput)

	// Layer 3: StrategyState beats StrategyUserInput
	r.WithState(deployedState())
	assertNamespace(t, r, "block-node-ns", StrategyState)
}

func assertNamespace(t *testing.T, r *BlockNodeRuntimeResolver, wantVal string, wantStrategy automa.EffectiveStrategy) {
	t.Helper()
	ns, err := r.Namespace()
	require.NoError(t, err)
	assert.Equal(t, wantVal, ns.Get().Val(), "namespace value mismatch")
	assert.Equal(t, wantStrategy, ns.Strategy(), "namespace strategy mismatch")
}

// ── chartVersionResolver ──────────────────────────────────────────────────────

func TestBlockNodeRuntimeResolver_ChartVersion_NoIntent_Errors(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	// intent deliberately not set

	_, err := r.ChartVersion()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "intent")
}

func TestBlockNodeRuntimeResolver_ChartVersion_Install_NotDeployed_UsesConfig(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)

	cv, err := r.ChartVersion()
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", cv.Get().Val())
	assert.Equal(t, StrategyConfig, cv.Strategy())
}

func TestBlockNodeRuntimeResolver_ChartVersion_Install_NotDeployed_UserInputWins(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)
	r.WithUserInputs(models.BlockNodeInputs{
		ChartVersion: "user-2.0.0", Namespace: "ns", Release: "rel", Chart: "oci://c",
	})

	cv, err := r.ChartVersion()
	require.NoError(t, err)
	assert.Equal(t, "user-2.0.0", cv.Get().Val())
	assert.Equal(t, StrategyUserInput, cv.Strategy())
}

func TestBlockNodeRuntimeResolver_ChartVersion_Install_Deployed_LockedToState(t *testing.T) {
	// Non-upgrade + deployed: state version is locked; user input cannot override.
	r := newTestResolver(fullConfig(), deployedState()) // state version = 0.29.0
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)
	r.WithUserInputs(models.BlockNodeInputs{
		ChartVersion: "user-99.0.0", Namespace: "ns", Release: "rel", Chart: "oci://c",
	})

	cv, err := r.ChartVersion()
	require.NoError(t, err)
	assert.Equal(t, "0.29.0", cv.Get().Val(), "state version must be locked for non-upgrade")
	assert.Equal(t, StrategyState, cv.Strategy())
}

func TestBlockNodeRuntimeResolver_ChartVersion_Upgrade_NotDeployed_Errors(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlockNode})

	_, err := r.ChartVersion()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not deployed")
}

func TestBlockNodeRuntimeResolver_ChartVersion_Upgrade_Deployed_UserInputWins(t *testing.T) {
	r := newTestResolver(fullConfig(), deployedState()) // state version = 0.29.0
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlockNode})
	r.WithUserInputs(models.BlockNodeInputs{
		ChartVersion: "0.30.0", Namespace: "ns", Release: "rel", Chart: "oci://c",
	})

	cv, err := r.ChartVersion()
	require.NoError(t, err)
	assert.Equal(t, "0.30.0", cv.Get().Val())
	assert.Equal(t, StrategyUserInput, cv.Strategy())
}

func TestBlockNodeRuntimeResolver_ChartVersion_Upgrade_Deployed_NoUserInput_FallsToConfig(t *testing.T) {
	r := newTestResolver(fullConfig(), deployedState()) // state version = 0.29.0, config version = 1.0.0
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlockNode})
	// no user input for chartVersion — config version should be preferred over deployed

	cv, err := r.ChartVersion()
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", cv.Get().Val(), "upgrade without user input uses config version over deployed version")
	assert.Equal(t, StrategyConfig, cv.Strategy())
}

func TestBlockNodeRuntimeResolver_ChartVersion_Intent_SwitchInvalidatesCache(t *testing.T) {
	r := newTestResolver(fullConfig(), deployedState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)

	cv, _ := r.ChartVersion()
	assert.Equal(t, StrategyState, cv.Strategy(), "install locks to state when deployed")

	// Switch to upgrade intent; user provides a new version
	r.WithIntent(models.Intent{Action: models.ActionUpgrade, Target: models.TargetBlockNode})
	r.WithUserInputs(models.BlockNodeInputs{
		ChartVersion: "0.30.0", Namespace: "ns", Release: "rel", Chart: "oci://c",
	})

	cv, err := r.ChartVersion()
	require.NoError(t, err)
	assert.Equal(t, "0.30.0", cv.Get().Val(), "upgrade with user input should use user input")
}

// ── validatedStringResolver ───────────────────────────────────────────────────

func TestBlockNodeRuntimeResolver_ValidatedString_EmptyDeployedNamespace_Errors(t *testing.T) {
	bad := deployedState()
	bad.ReleaseInfo.Namespace = ""

	r := newTestResolver(fullConfig(), bad)
	r.WithDefaults(testDefaultsConfig())

	_, err := r.Namespace()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "namespace")
}

func TestBlockNodeRuntimeResolver_ValidatedString_EmptyDeployedReleaseName_Errors(t *testing.T) {
	bad := deployedState()
	bad.ReleaseInfo.Name = ""

	r := newTestResolver(fullConfig(), bad)
	r.WithDefaults(testDefaultsConfig())

	_, err := r.ReleaseName()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "release name")
}

func TestBlockNodeRuntimeResolver_ValidatedString_EmptyDeployedChartName_Errors(t *testing.T) {
	bad := deployedState()
	bad.ReleaseInfo.ChartName = ""

	r := newTestResolver(fullConfig(), bad)
	r.WithDefaults(testDefaultsConfig())

	_, err := r.ChartName()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "chart name")
}

// ── chartRefResolver ──────────────────────────────────────────────────────────

func TestBlockNodeRuntimeResolver_ChartRef_EmptyDeployed_SoftFallThrough(t *testing.T) {
	// Deployed release has an empty ChartRef (not stored in Helm metadata).
	st := deployedState()
	st.ReleaseInfo.ChartRef = ""

	r := newTestResolver(fullConfig(), st)
	r.WithDefaults(testDefaultsConfig())

	// Must not error — falls through to config
	cr, err := r.ChartRef()
	require.NoError(t, err)
	assert.Equal(t, "oci://config/chart", cr.Get().Val())
	assert.Equal(t, StrategyConfig, cr.Strategy())
}

func TestBlockNodeRuntimeResolver_ChartRef_NonEmptyDeployed_UsesState(t *testing.T) {
	r := newTestResolver(fullConfig(), deployedState())
	r.WithDefaults(testDefaultsConfig())

	cr, err := r.ChartRef()
	require.NoError(t, err)
	assert.Equal(t, "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server", cr.Get().Val())
	assert.Equal(t, StrategyState, cr.Strategy())
}

// ── storageResolver ───────────────────────────────────────────────────────────

func TestStorageResolver_UserInputOnly(t *testing.T) {
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)
	r.WithUserInputs(models.BlockNodeInputs{
		Namespace: "ns", Release: "rel", Chart: "oci://c", ChartVersion: "1.0.0",
		Storage: models.BlockNodeStorage{
			BasePath: "/user/base",
			LiveSize: "5Gi",
		},
	})

	st, err := r.Storage()
	require.NoError(t, err)
	assert.Equal(t, "/user/base", st.Get().Val().BasePath)
	assert.Equal(t, "5Gi", st.Get().Val().LiveSize)
	assert.Equal(t, StrategyUserInput, st.Strategy())
}

func TestStorageResolver_StateFillsGaps(t *testing.T) {
	// Deployed state has BasePath and LiveSize;
	// user supplies only ArchiveSize.
	r := newTestResolver(models.Config{}, deployedState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)
	r.WithUserInputs(models.BlockNodeInputs{
		Namespace: "ns", Release: "rel", Chart: "oci://c", ChartVersion: "1.0.0",
		Storage: models.BlockNodeStorage{ArchiveSize: "20Gi"},
	})

	st, err := r.Storage()
	require.NoError(t, err)

	// User input is the leading strategy
	assert.Equal(t, StrategyUserInput, st.Strategy())
	// State fills gaps
	assert.Equal(t, "/mnt/fast-storage", st.Get().Val().BasePath, "BasePath from state")
	assert.Equal(t, "10Gi", st.Get().Val().LiveSize, "LiveSize from state")
	assert.Equal(t, "20Gi", st.Get().Val().ArchiveSize, "ArchiveSize from user input")
}

func TestStorageResolver_DefaultFillsGaps(t *testing.T) {
	// No user input, no deployed state, no config storage; default provides BasePath.
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig()) // BasePath = /mnt/fast-storage
	r.WithIntent(installIntent)

	st, err := r.Storage()
	require.NoError(t, err)

	// Default BasePath must have been merged in.
	assert.Equal(t, "/mnt/fast-storage", st.Get().Val().BasePath)
	// Strategy must reflect that the default was the actual contributor, not config.
	assert.Equal(t, StrategyDefault, st.Strategy(), "strategy must be StrategyDefault when only defaults contribute")
}

func TestStorageResolver_CorruptDeployedStorage_Errors(t *testing.T) {
	// Deployed state with an invalid (non-absolute) BasePath fails Validate().
	bad := deployedState()
	bad.Storage.BasePath = "../../etc/passwd"

	r := newTestResolver(models.Config{}, bad)
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)

	_, err := r.Storage()
	require.Error(t, err, "corrupt deployed storage must produce an error")
}

// ── RefreshState ──────────────────────────────────────────────────────────────

func TestBlockNodeRuntimeResolver_RefreshState_AlwaysStale_CallsChecker(t *testing.T) {
	checker := &mockBlockNodeChecker{state: deployedState()}
	r, err := NewBlockNodeRuntimeResolver(models.Config{}, state.NewBlockNodeState(), checker, 0) // interval=0 → always stale
	require.NoError(t, err)
	r.(*BlockNodeRuntimeResolver).WithDefaults(testDefaultsConfig())

	require.NoError(t, r.RefreshState(context.Background(), false))
	assert.Equal(t, 1, checker.calls)

	require.NoError(t, r.RefreshState(context.Background(), false))
	assert.Equal(t, 2, checker.calls, "second call must also hit the checker when always stale")
}

func TestBlockNodeRuntimeResolver_RefreshState_Fresh_Skips(t *testing.T) {
	checker := &mockBlockNodeChecker{state: deployedState()}
	r, err := NewBlockNodeRuntimeResolver(models.Config{}, state.NewBlockNodeState(), checker, 24*time.Hour)
	require.NoError(t, err)
	br := r.(*BlockNodeRuntimeResolver)
	br.WithDefaults(testDefaultsConfig())

	// Seed a freshly-synced state so the staleness check passes.
	freshState := state.NewBlockNodeState()
	freshState.LastSync = htime.Now()
	br.WithState(freshState)

	require.NoError(t, r.RefreshState(context.Background(), false))
	assert.Equal(t, 0, checker.calls, "fresh state must skip the reality checker")
}

func TestBlockNodeRuntimeResolver_RefreshState_Force_BypassesStaleness(t *testing.T) {
	checker := &mockBlockNodeChecker{state: deployedState()}
	r, err := NewBlockNodeRuntimeResolver(models.Config{}, state.NewBlockNodeState(), checker, 24*time.Hour)
	require.NoError(t, err)
	br := r.(*BlockNodeRuntimeResolver)
	br.WithDefaults(testDefaultsConfig())

	freshState := state.NewBlockNodeState()
	freshState.LastSync = htime.Now()
	br.WithState(freshState)

	// force=true should call checker even when state is fresh
	require.NoError(t, r.RefreshState(context.Background(), true))
	assert.Equal(t, 1, checker.calls)
}

func TestBlockNodeRuntimeResolver_RefreshState_SeedsRealitySources(t *testing.T) {
	reality := deployedState()
	checker := &mockBlockNodeChecker{state: reality}
	r, err := NewBlockNodeRuntimeResolver(fullConfig(), state.NewBlockNodeState(), checker, 0)
	require.NoError(t, err)
	br := r.(*BlockNodeRuntimeResolver)
	br.WithDefaults(testDefaultsConfig())
	br.WithIntent(installIntent)

	require.NoError(t, r.RefreshState(context.Background(), true))

	ns, err := br.Namespace()
	require.NoError(t, err)

	realityVal, _ := ns.RealityVal()
	assert.Equal(t, "block-node-ns", realityVal.Val())
	assert.Equal(t, StrategyReality, ns.Strategy(), "reality must be the winning strategy after refresh")
}

func TestBlockNodeRuntimeResolver_RefreshState_CheckerError_Propagates(t *testing.T) {
	checker := &mockBlockNodeChecker{err: assert.AnError}
	r, err := NewBlockNodeRuntimeResolver(models.Config{}, state.NewBlockNodeState(), checker, 0)
	require.NoError(t, err)

	// errorx.Wrap does not preserve the Go error chain through errors.Is;
	// check that the underlying message is present in the wrapped error instead.
	refreshErr := r.RefreshState(context.Background(), true)
	require.Error(t, refreshErr)
	assert.Contains(t, refreshErr.Error(), assert.AnError.Error())
}

// ── CurrentState ──────────────────────────────────────────────────────────────

func TestBlockNodeRuntimeResolver_CurrentState_ReturnsConstructorState(t *testing.T) {
	st := deployedState()
	r := newTestResolver(fullConfig(), st)

	got, err := r.CurrentState()
	require.NoError(t, err)
	assert.Equal(t, st.ReleaseInfo.Name, got.ReleaseInfo.Name)
	assert.Equal(t, st.ReleaseInfo.Namespace, got.ReleaseInfo.Namespace)
	assert.Equal(t, st.ReleaseInfo.Status, got.ReleaseInfo.Status)
}

func TestBlockNodeRuntimeResolver_CurrentState_UpdatedByWithState(t *testing.T) {
	r := newTestResolver(fullConfig(), state.NewBlockNodeState())

	newState := deployedState()
	r.WithState(newState)

	got, err := r.CurrentState()
	require.NoError(t, err)
	assert.Equal(t, newState.ReleaseInfo.Name, got.ReleaseInfo.Name)
	assert.Equal(t, release.StatusDeployed, got.ReleaseInfo.Status)
}

// ── WithConfig bug regression ─────────────────────────────────────────────────
//
// Bug: WithConfig previously called SetSource(StrategyConfig, "") unconditionally,
// even when a field was absent from config.yaml (empty string).  Because the
// resolvers use a presence-only check (if v, ok := sources[st]; ok), the empty
// StrategyConfig entry shadowed StrategyDefault, so defaults were never reached.

func TestBlockNodeRuntimeResolver_EmptyConfig_FallsBackToDefault_Namespace(t *testing.T) {
	// Arrange: empty config (no fields in config.yaml), only defaults set.
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)

	ns, err := r.Namespace()
	require.NoError(t, err)
	// Must resolve to the default, not to an empty string from an absent config field.
	assert.Equal(t, "block-node", ns.Get().Val(), "empty config must not shadow default namespace")
	assert.Equal(t, StrategyDefault, ns.Strategy())
}

func TestBlockNodeRuntimeResolver_EmptyConfig_FallsBackToDefault_ReleaseName(t *testing.T) {
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)

	rel, err := r.ReleaseName()
	require.NoError(t, err)
	assert.Equal(t, "block-node", rel.Get().Val(), "empty config must not shadow default release name")
	assert.Equal(t, StrategyDefault, rel.Strategy())
}

func TestBlockNodeRuntimeResolver_EmptyConfig_FallsBackToDefault_ChartVersion(t *testing.T) {
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)

	cv, err := r.ChartVersion()
	require.NoError(t, err)
	assert.Equal(t, "0.29.0", cv.Get().Val(), "empty config must not shadow default chart version")
	assert.Equal(t, StrategyDefault, cv.Strategy())
}

func TestBlockNodeRuntimeResolver_EmptyConfig_FallsBackToDefault_ChartRef(t *testing.T) {
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)

	cr, err := r.ChartRef()
	require.NoError(t, err)
	assert.Equal(t, "oci://ghcr.io/hiero-ledger/hiero-block-node/block-node-server", cr.Get().Val(),
		"empty config must not shadow default chart ref")
	assert.Equal(t, StrategyDefault, cr.Strategy())
}

func TestBlockNodeRuntimeResolver_WithConfig_NonEmptyField_WinsOverDefault(t *testing.T) {
	// Partial config: only namespace is set. All other fields fall back to default.
	r := newTestResolver(models.Config{
		BlockNode: models.BlockNodeConfig{Namespace: "my-ns"},
	}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig())
	r.WithIntent(installIntent)

	ns, err := r.Namespace()
	require.NoError(t, err)
	assert.Equal(t, "my-ns", ns.Get().Val(), "non-empty config field must win over default")
	assert.Equal(t, StrategyConfig, ns.Strategy())

	// Unset fields must still fall back to default.
	rel, err := r.ReleaseName()
	require.NoError(t, err)
	assert.Equal(t, "block-node", rel.Get().Val(), "field absent from config must fall back to default")
	assert.Equal(t, StrategyDefault, rel.Strategy())
}

func TestBlockNodeRuntimeResolver_WithConfig_EmptyStorage_FallsBackToDefault(t *testing.T) {
	// Config has no storage section; default has BasePath.
	r := newTestResolver(models.Config{}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig()) // BasePath = /mnt/fast-storage
	r.WithIntent(installIntent)

	st, err := r.Storage()
	require.NoError(t, err)
	assert.Equal(t, "/mnt/fast-storage", st.Get().Val().BasePath,
		"empty config storage must not shadow default BasePath")
	assert.Equal(t, StrategyDefault, st.Strategy())
}

func TestStorageResolver_ConfigStorageWinsOverDefault(t *testing.T) {
	// Config provides its own BasePath; default also provides one.
	// Config must win because it has higher precedence than Default.
	r := newTestResolver(models.Config{
		BlockNode: models.BlockNodeConfig{
			Storage: models.BlockNodeStorage{BasePath: "/mnt/config-base"},
		},
	}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig()) // BasePath = /mnt/fast-storage
	r.WithIntent(installIntent)

	st, err := r.Storage()
	require.NoError(t, err)
	assert.Equal(t, "/mnt/config-base", st.Get().Val().BasePath,
		"config BasePath must win over default BasePath")
	assert.Equal(t, StrategyConfig, st.Strategy())
}

func TestStorageResolver_ConfigPartial_DefaultFillsRemainingGaps(t *testing.T) {
	// Config provides ArchivePath but not BasePath; default provides BasePath.
	r := newTestResolver(models.Config{
		BlockNode: models.BlockNodeConfig{
			Storage: models.BlockNodeStorage{ArchivePath: "/mnt/config-archive"},
		},
	}, state.NewBlockNodeState())
	r.WithDefaults(testDefaultsConfig()) // BasePath = /mnt/fast-storage
	r.WithIntent(installIntent)

	st, err := r.Storage()
	require.NoError(t, err)
	// Config's ArchivePath is present.
	assert.Equal(t, "/mnt/config-archive", st.Get().Val().ArchivePath, "ArchivePath from config")
	// Default fills the gap for BasePath.
	assert.Equal(t, "/mnt/fast-storage", st.Get().Val().BasePath, "BasePath from default fills gap")
	// Winning strategy is Config because that was the highest-priority non-empty source.
	assert.Equal(t, StrategyConfig, st.Strategy())
}
