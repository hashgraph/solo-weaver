// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"sync"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/deps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

// TeleportRuntimeResolver manages the teleport state lifecycle and provides
// per-field effective value resolution for teleport configuration.
//
// It uses TeleportNodeInputs as the generic input type for the Resolver interface.
// Cluster inputs are handled via the concrete WithClusterInputs method — callers
// must type-assert to *TeleportRuntimeResolver (consistent with how blocknode
// handlers type-assert to *BlockNodeRuntimeResolver).
type TeleportRuntimeResolver struct {
	mu              sync.Mutex
	state           *state.TeleportState
	refreshInterval time.Duration
	realityChecker  reality.Checker[state.TeleportState]
	intent          *models.Intent

	// Node agent fields
	token     *EffectiveValue[string]
	proxyAddr *EffectiveValue[string]

	// Cluster agent fields
	version     *EffectiveValue[string]
	valuesFile  *EffectiveValue[string]
	namespace   *EffectiveValue[string]
	releaseName *EffectiveValue[string]
}

// ── Builder / source-setter methods ──────────────────────────────────────────

func (t *TeleportRuntimeResolver) WithIntent(intent models.Intent) Resolver[state.TeleportState, models.TeleportNodeInputs] {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.intent = &intent
	return t
}

func (t *TeleportRuntimeResolver) WithUserInputs(inputs models.TeleportNodeInputs) Resolver[state.TeleportState, models.TeleportNodeInputs] {
	t.mu.Lock()
	defer t.mu.Unlock()

	setOrClearString(t.token, StrategyUserInput, inputs.Token)
	setOrClearString(t.proxyAddr, StrategyUserInput, inputs.ProxyAddr)
	return t
}

// WithClusterInputs registers cluster agent user inputs as StrategyUserInput sources.
// This method lives on the concrete struct because the Resolver[S, I] interface is
// typed with TeleportNodeInputs — cluster handlers must type-assert to access it.
func (t *TeleportRuntimeResolver) WithClusterInputs(inputs models.TeleportClusterInputs) *TeleportRuntimeResolver {
	t.mu.Lock()
	defer t.mu.Unlock()

	setOrClearString(t.version, StrategyUserInput, inputs.Version)
	setOrClearString(t.valuesFile, StrategyUserInput, inputs.ValuesFile)
	return t
}

func (t *TeleportRuntimeResolver) WithConfig(cfg models.Config) Resolver[state.TeleportState, models.TeleportNodeInputs] {
	t.mu.Lock()
	defer t.mu.Unlock()

	setOrClearString(t.token, StrategyConfig, cfg.Teleport.NodeAgentToken)
	setOrClearString(t.proxyAddr, StrategyConfig, cfg.Teleport.NodeAgentProxyAddr)
	setOrClearString(t.version, StrategyConfig, cfg.Teleport.Version)
	setOrClearString(t.valuesFile, StrategyConfig, cfg.Teleport.ValuesFile)
	return t
}

// WithDefaults registers hardcoded compile-time constants as StrategyDefault sources.
// cfg should be the result of config.DefaultsConfig().
func (t *TeleportRuntimeResolver) WithDefaults(cfg models.Config) Resolver[state.TeleportState, models.TeleportNodeInputs] {
	t.mu.Lock()
	defer t.mu.Unlock()

	setOrClearString(t.version, StrategyDefault, cfg.Teleport.Version)
	// namespace and releaseName defaults are seeded in the constructor from deps constants
	return t
}

// WithEnv registers env-var-sourced values as StrategyEnv sources.
// cfg should be the result of config.EnvConfig().
func (t *TeleportRuntimeResolver) WithEnv(cfg models.Config) Resolver[state.TeleportState, models.TeleportNodeInputs] {
	t.mu.Lock()
	defer t.mu.Unlock()

	setOrClearString(t.token, StrategyEnv, cfg.Teleport.NodeAgentToken)
	setOrClearString(t.proxyAddr, StrategyEnv, cfg.Teleport.NodeAgentProxyAddr)
	setOrClearString(t.version, StrategyEnv, cfg.Teleport.Version)
	setOrClearString(t.valuesFile, StrategyEnv, cfg.Teleport.ValuesFile)
	return t
}

func (t *TeleportRuntimeResolver) WithState(st state.TeleportState) Resolver[state.TeleportState, models.TeleportNodeInputs] {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.state = &st
	t.setStateSources(st, StrategyState)
	return t
}

// setStateSources pushes cluster agent state values into the per-field resolvers
// under the given strategy tier (StrategyState or StrategyReality).
// Token and proxyAddr are never sourced from state (sensitive / config-only).
// Must be called with t.mu held.
func (t *TeleportRuntimeResolver) setStateSources(st state.TeleportState, strategy automa.EffectiveStrategy) {
	if !st.ClusterAgent.Installed {
		t.version.ClearSource(strategy)
		t.namespace.ClearSource(strategy)
		t.releaseName.ClearSource(strategy)
		return
	}

	setOrClearString(t.version, strategy, st.ClusterAgent.ChartVersion)
	setOrClearString(t.namespace, strategy, st.ClusterAgent.Namespace)
	setOrClearString(t.releaseName, strategy, st.ClusterAgent.Release)
}

// ── Field accessor methods ────────────────────────────────────────────────────

// Token returns the effective token resolver.
func (t *TeleportRuntimeResolver) Token() (*EffectiveValue[string], error) {
	if _, err := t.token.Resolve(); err != nil {
		return nil, err
	}
	return t.token, nil
}

// ProxyAddr returns the effective proxy address resolver.
func (t *TeleportRuntimeResolver) ProxyAddr() (*EffectiveValue[string], error) {
	if _, err := t.proxyAddr.Resolve(); err != nil {
		return nil, err
	}
	return t.proxyAddr, nil
}

// Version returns the effective chart version resolver for the cluster agent.
func (t *TeleportRuntimeResolver) Version() (*EffectiveValue[string], error) {
	if _, err := t.version.Resolve(); err != nil {
		return nil, err
	}
	return t.version, nil
}

// ValuesFile returns the effective Helm values file resolver for the cluster agent.
func (t *TeleportRuntimeResolver) ValuesFile() (*EffectiveValue[string], error) {
	if _, err := t.valuesFile.Resolve(); err != nil {
		return nil, err
	}
	return t.valuesFile, nil
}

// Namespace returns the effective namespace resolver for the cluster agent.
func (t *TeleportRuntimeResolver) Namespace() (*EffectiveValue[string], error) {
	if _, err := t.namespace.Resolve(); err != nil {
		return nil, err
	}
	return t.namespace, nil
}

// ReleaseName returns the effective release name resolver for the cluster agent.
func (t *TeleportRuntimeResolver) ReleaseName() (*EffectiveValue[string], error) {
	if _, err := t.releaseName.Resolve(); err != nil {
		return nil, err
	}
	return t.releaseName, nil
}

// ── State refresh ─────────────────────────────────────────────────────────────

func (t *TeleportRuntimeResolver) RefreshState(ctx context.Context, force bool) error {
	logx.As().Debug().Msg("Refreshing teleport state using reality checker")

	now := htime.Now()
	if !force {
		t.mu.Lock()
		if t.state != nil && now.Sub(t.state.LastSync) < t.refreshInterval {
			t.mu.Unlock()
			logx.As().Debug().
				Time("lastSync", t.state.LastSync.Time).
				Dur("refreshInterval", t.refreshInterval).
				Dur("timeSinceLastSync", now.Sub(t.state.LastSync)).
				Msg("Teleport state is fresh; skipping refresh")
			return nil
		}
		t.mu.Unlock()
	}

	st, err := t.realityChecker.RefreshState(ctx)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to refresh teleport state")
	}
	st.LastSync = now

	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = &st
	t.setStateSources(st, StrategyReality)

	logx.As().Debug().Any("state", t.state).Msg("Refreshed teleport state using reality checker")
	return nil
}

func (t *TeleportRuntimeResolver) CurrentState() (state.TeleportState, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.state == nil {
		return state.TeleportState{}, errorx.IllegalState.New("teleport state is not initialized")
	}

	return *t.state, nil
}

// ── Constructor ───────────────────────────────────────────────────────────────

// NewTeleportRuntimeResolver creates a TeleportRuntimeResolver with per-field
// EffectiveValue resolvers. All fields use DefaultSelector (standard priority walk).
func NewTeleportRuntimeResolver(
	cfg models.Config,
	teleportState state.TeleportState,
	realityChecker reality.Checker[state.TeleportState],
	refreshInterval time.Duration,
) (Resolver[state.TeleportState, models.TeleportNodeInputs], error) {
	tr := &TeleportRuntimeResolver{
		state:           &teleportState,
		refreshInterval: refreshInterval,
		realityChecker:  realityChecker,
	}

	var err error

	// Node agent fields — DefaultSelector, no state/reality sources (sensitive)
	tr.token, err = NewEffectiveValue[string](nil)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create token resolver")
	}
	tr.proxyAddr, err = NewEffectiveValue[string](nil)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create proxyAddr resolver")
	}

	// Cluster agent fields — DefaultSelector
	tr.version, err = NewEffectiveValue[string](nil)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create version resolver")
	}
	tr.valuesFile, err = NewEffectiveValue[string](nil)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create valuesFile resolver")
	}
	tr.namespace, err = NewEffectiveValue[string](nil)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create namespace resolver")
	}
	tr.releaseName, err = NewEffectiveValue[string](nil)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create releaseName resolver")
	}

	// Seed namespace and releaseName defaults from deps constants
	// (these are not user-configurable via config or CLI)
	setOrClearString(tr.namespace, StrategyDefault, deps.TELEPORT_NAMESPACE)
	setOrClearString(tr.releaseName, StrategyDefault, deps.TELEPORT_RELEASE)

	// Initialize sources from config and state
	tr.WithConfig(cfg)
	tr.WithState(teleportState)

	return tr, nil
}
