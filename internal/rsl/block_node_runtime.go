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
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	htime "helm.sh/helm/v3/pkg/time"
)

// ── Custom resolver types ─────────────────────────────────────────────────────

// validatedStringResolver errors when a deployed state value is empty.
// Used for fields that must be non-empty when a release is deployed
// (namespace, release name, chart name).
type validatedStringResolver struct {
	fieldName string
}

func (r *validatedStringResolver) Resolve(sources map[automa.EffectiveStrategy]automa.Value[string]) (*automa.EffectiveValue[string], error) {
	for _, st := range []automa.EffectiveStrategy{StrategyReality, StrategyState} {
		if v, ok := sources[st]; ok {
			if v.Val() == "" {
				return nil, errorx.IllegalState.New(
					"block node runtime state is inconsistent: deployed release cannot have empty %s", r.fieldName)
			}
			return automa.NewEffectiveValue(v, st)
		}
	}
	for _, st := range []automa.EffectiveStrategy{StrategyUserInput, StrategyEnv, StrategyConfig, StrategyDefault} {
		if v, ok := sources[st]; ok {
			return automa.NewEffectiveValue(v, st)
		}
	}
	return automa.NewEffective("", StrategyZero)
}

// chartRefResolver soft-falls through on an empty deployed ChartRef (logs a warning
// instead of erroring) then falls back to user input or config.
type chartRefResolver struct{}

func (r *chartRefResolver) Resolve(sources map[automa.EffectiveStrategy]automa.Value[string]) (*automa.EffectiveValue[string], error) {
	for _, st := range []automa.EffectiveStrategy{StrategyReality, StrategyState} {
		if v, ok := sources[st]; ok {
			if v.Val() != "" {
				return automa.NewEffectiveValue(v, st)
			}
			logx.As().Warn().
				Str("strategy", StrategyName(st)).
				Msg("deployed release has empty chart ref; falling back to user input or config")
		}
	}
	for _, st := range []automa.EffectiveStrategy{StrategyUserInput, StrategyEnv, StrategyConfig, StrategyDefault} {
		if v, ok := sources[st]; ok {
			return automa.NewEffectiveValue(v, st)
		}
	}
	return automa.NewEffective("", StrategyZero)
}

// chartVersionResolver is intent-aware.
//   - Upgrade:     UserInput > Reality > State > Config > Default
//     (requires a deployed release; errors if none)
//   - Non-upgrade: Reality/State is authoritative when deployed (locked, non-empty);
//     falls back to UserInput > Config > Default when not deployed.
//
// It closes over **models.Intent so that WithIntent updates are always visible
// without needing to rebuild the resolver.
type chartVersionResolver struct {
	intent **models.Intent
}

func (r *chartVersionResolver) Resolve(sources map[automa.EffectiveStrategy]automa.Value[string]) (*automa.EffectiveValue[string], error) {
	if r.intent == nil || *r.intent == nil {
		return nil, errorx.IllegalState.New("intent is not set in block node runtime resolver")
	}
	intent := **r.intent

	stateDeployed := false
	for _, st := range []automa.EffectiveStrategy{StrategyReality, StrategyState} {
		if _, ok := sources[st]; ok {
			stateDeployed = true
			break
		}
	}

	if intent.Action == models.ActionUpgrade {
		if !stateDeployed {
			return nil, errorx.IllegalState.New(
				"block node is not deployed; cannot perform upgrade").
				WithProperty(models.ErrPropertyResolution,
					"upgrade action requires a currently deployed block node release")
		}
		// Upgrade: UserInput can override the deployed version.
		for _, st := range []automa.EffectiveStrategy{StrategyUserInput, StrategyReality, StrategyState, StrategyConfig, StrategyDefault} {
			if v, ok := sources[st]; ok {
				return automa.NewEffectiveValue(v, st)
			}
		}
		return nil, errorx.IllegalState.New("no chart version available for upgrade")
	}

	// Non-upgrade: deployed version is authoritative.
	for _, st := range []automa.EffectiveStrategy{StrategyReality, StrategyState} {
		if v, ok := sources[st]; ok {
			if v.Val() == "" {
				return nil, errorx.IllegalState.New(
					"block node runtime state is inconsistent: deployed release cannot have empty chart version")
			}
			return automa.NewEffectiveValue(v, st)
		}
	}

	// Not deployed — fall back normally.
	for _, st := range []automa.EffectiveStrategy{StrategyUserInput, StrategyEnv, StrategyConfig, StrategyDefault} {
		if v, ok := sources[st]; ok {
			return automa.NewEffectiveValue(v, st)
		}
	}
	return automa.NewEffective("", StrategyZero)
}

// storageResolver merges storage fields from all sources using the same
// MergeFrom cascade as the main-branch imperative Storage() method:
//
//  1. User input (highest priority for any field the operator set)
//  2. Reality / State (fills gaps; validated when deployed)
//  3. Config → Default (fills any remaining empty fields)
//
// Strategy reflects the highest-priority source that contributed any field.
type storageResolver struct{}

func (r *storageResolver) Resolve(sources map[automa.EffectiveStrategy]automa.Value[models.BlockNodeStorage]) (*automa.EffectiveValue[models.BlockNodeStorage], error) {
	var storage models.BlockNodeStorage
	strategy := StrategyZero

	// 1. Start with user input — operator-supplied fields win over everything.
	if uv, ok := sources[StrategyUserInput]; ok {
		userStorage := uv.Val()
		if !userStorage.IsEmpty() {
			storage = userStorage
			strategy = StrategyUserInput
		}
	}

	// 2. Fill gaps from reality / state (whichever is present).
	//    Validate the deployed storage before merging to catch corrupt state.
	for _, st := range []automa.EffectiveStrategy{StrategyReality, StrategyState} {
		if sv, ok := sources[st]; ok {
			stateStorage := sv.Val()
			if err := stateStorage.Validate(); err != nil {
				return nil, errorx.IllegalState.Wrap(err, "block node runtime state is inconsistent")
			}
			storage.MergeFrom(stateStorage)
			if strategy == StrategyZero {
				strategy = st
			}
			break
		}
	}

	// 3. Fill any remaining gaps from config, then default.
	for _, st := range []automa.EffectiveStrategy{StrategyConfig, StrategyDefault} {
		if cv, ok := sources[st]; ok {
			storage.MergeFrom(cv.Val())
		}
	}

	if strategy == StrategyZero {
		strategy = StrategyConfig // nothing was deployed and no user input
	}

	sv, err := automa.NewValue(storage)
	if err != nil {
		return nil, err
	}
	return automa.NewEffectiveValue(sv, strategy)
}

// ── BlockNodeRuntimeResolver ──────────────────────────────────────────────────

// BlockNodeRuntimeResolver manages the current state of block-node related information.
//
// Each resolvable field owns an *EffectiveValueResolver[T] that holds all value
// sources in a map keyed by strategy.  Resolution precedence (highest → lowest):
//
//	StrategyReality   – live cluster (from RefreshState)
//	StrategyState     – persisted state on disk (from WithState)
//	StrategyUserInput – CLI flags (from WithUserInputs)
//	StrategyEnv       – environment variables (set externally via SetSource)
//	StrategyConfig    – config file (from WithConfig)
//	StrategyDefault   – hardcoded deps.* constants (seeded at construction)
//	StrategyZero      – zero value (ultimate fallback)
type BlockNodeRuntimeResolver struct {
	mu              sync.Mutex
	state           *state.BlockNodeState // raw snapshot for CurrentState() and staleness checks
	refreshInterval time.Duration
	realityChecker  reality.Checker[state.BlockNodeState]
	intent          *models.Intent

	// Per-field effective values — sources are kept up-to-date by With* / RefreshState.
	namespace    *EffectiveValue[string]
	releaseName  *EffectiveValue[string]
	chartName    *EffectiveValue[string]
	chartRef     *EffectiveValue[string]
	chartVersion *EffectiveValue[string]
	storage      *EffectiveValue[models.BlockNodeStorage]
}

// ── Builder / source-setter methods ──────────────────────────────────────────

func (b *BlockNodeRuntimeResolver) WithIntent(intent models.Intent) Resolver[state.BlockNodeState, models.BlockNodeInputs] {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.intent = &intent
	// Intent affects chartVersion resolution — invalidate so next Resolve() sees the new intent.
	b.chartVersion.Invalidate()
	return b
}

func (b *BlockNodeRuntimeResolver) WithUserInputs(inputs models.BlockNodeInputs) Resolver[state.BlockNodeState, models.BlockNodeInputs] {
	b.mu.Lock()
	defer b.mu.Unlock()

	setOrClearString(b.namespace, StrategyUserInput, inputs.Namespace)
	setOrClearString(b.releaseName, StrategyUserInput, inputs.Release)
	setOrClearString(b.chartName, StrategyUserInput, inputs.ChartName)
	setOrClearString(b.chartRef, StrategyUserInput, inputs.Chart)
	setOrClearString(b.chartVersion, StrategyUserInput, inputs.ChartVersion)

	if err := inputs.Storage.Validate(); err == nil {
		_ = b.storage.SetSource(StrategyUserInput, inputs.Storage)
	} else {
		b.storage.ClearSource(StrategyUserInput)
	}
	return b
}

func (b *BlockNodeRuntimeResolver) WithConfig(cfg models.Config) Resolver[state.BlockNodeState, models.BlockNodeInputs] {
	b.mu.Lock()
	defer b.mu.Unlock()

	_ = b.namespace.SetSource(StrategyConfig, cfg.BlockNode.Namespace)
	_ = b.releaseName.SetSource(StrategyConfig, cfg.BlockNode.Release)
	_ = b.chartName.SetSource(StrategyConfig, cfg.BlockNode.ChartName)
	_ = b.chartRef.SetSource(StrategyConfig, cfg.BlockNode.Chart)
	_ = b.chartVersion.SetSource(StrategyConfig, cfg.BlockNode.ChartVersion)
	_ = b.storage.SetSource(StrategyConfig, cfg.BlockNode.Storage)
	return b
}

func (b *BlockNodeRuntimeResolver) WithState(st state.BlockNodeState) Resolver[state.BlockNodeState, models.BlockNodeInputs] {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.state = &st
	b.setStateSources(st, StrategyState)
	return b
}

// setStateSources pushes state values into the per-field resolvers under the
// given strategy tier (StrategyState or StrategyReality).
// Must be called with b.mu held.
func (b *BlockNodeRuntimeResolver) setStateSources(st state.BlockNodeState, strategy automa.EffectiveStrategy) {
	if st.ReleaseInfo.Status != release.StatusDeployed {
		b.namespace.ClearSource(strategy)
		b.releaseName.ClearSource(strategy)
		b.chartName.ClearSource(strategy)
		b.chartRef.ClearSource(strategy)
		b.chartVersion.ClearSource(strategy)
		b.storage.ClearSource(strategy)
		return
	}

	_ = b.namespace.SetSource(strategy, st.ReleaseInfo.Namespace)
	_ = b.releaseName.SetSource(strategy, st.ReleaseInfo.Name)
	_ = b.chartName.SetSource(strategy, st.ReleaseInfo.ChartName)
	_ = b.chartRef.SetSource(strategy, st.ReleaseInfo.ChartRef)
	_ = b.chartVersion.SetSource(strategy, st.ReleaseInfo.ChartVersion)
	_ = b.storage.SetSource(strategy, st.Storage)
}

// ── Field accessor methods ────────────────────────────────────────────────────

// Namespace returns the effective namespace resolver.
// The resolver carries all source layers; call Get().Val() for the winning value
// or the source-specific Val methods (StateVal, UserInputVal, …) to inspect layers.
func (b *BlockNodeRuntimeResolver) Namespace() (*EffectiveValue[string], error) {
	if _, err := b.namespace.Resolve(); err != nil {
		return nil, err
	}
	return b.namespace, nil
}

func (b *BlockNodeRuntimeResolver) ReleaseName() (*EffectiveValue[string], error) {
	if _, err := b.releaseName.Resolve(); err != nil {
		return nil, err
	}
	return b.releaseName, nil
}

func (b *BlockNodeRuntimeResolver) ChartName() (*EffectiveValue[string], error) {
	if _, err := b.chartName.Resolve(); err != nil {
		return nil, err
	}
	return b.chartName, nil
}

func (b *BlockNodeRuntimeResolver) ChartRef() (*EffectiveValue[string], error) {
	if _, err := b.chartRef.Resolve(); err != nil {
		return nil, err
	}
	return b.chartRef, nil
}

func (b *BlockNodeRuntimeResolver) ChartVersion() (*EffectiveValue[string], error) {
	if _, err := b.chartVersion.Resolve(); err != nil {
		return nil, err
	}
	return b.chartVersion, nil
}

func (b *BlockNodeRuntimeResolver) Storage() (*EffectiveValue[models.BlockNodeStorage], error) {
	if _, err := b.storage.Resolve(); err != nil {
		return nil, err
	}
	return b.storage, nil
}

// ── State refresh ─────────────────────────────────────────────────────────────

func (b *BlockNodeRuntimeResolver) RefreshState(ctx context.Context, force bool) error {
	logx.As().Debug().Msg("Refreshing block node state using reality checker")

	now := htime.Now()
	if !force {
		b.mu.Lock()
		if b.state != nil && now.Sub(b.state.LastSync) < b.refreshInterval {
			b.mu.Unlock()
			logx.As().Debug().
				Time("lastSync", b.state.LastSync.Time).
				Dur("refreshInterval", b.refreshInterval).
				Dur("timeSinceLastSync", now.Sub(b.state.LastSync)).
				Msg("Block node state is fresh; skipping refresh")
			return nil
		}
		b.mu.Unlock()
	}

	st, err := b.realityChecker.RefreshState(ctx)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to refresh block node state")
	}
	st.LastSync = now

	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = &st
	// Reality sources take precedence over State sources; the chartRefResolver
	// handles an empty reality ChartRef by warning and falling through to
	// StrategyState (which retains the previously known ChartRef).
	b.setStateSources(st, StrategyReality)

	logx.As().Debug().Any("state", b.state).Msg("Refreshed block node state using reality checker")
	return nil
}

func (b *BlockNodeRuntimeResolver) CurrentState() (state.BlockNodeState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == nil {
		return state.NewBlockNodeState(), errorx.IllegalState.New("block node state is not initialized")
	}
	return *b.state, nil
}

// ── Constructor ───────────────────────────────────────────────────────────────

func NewBlockNodeRuntimeResolver(
	cfg models.Config,
	blockNodeState state.BlockNodeState,
	realityChecker reality.Checker[state.BlockNodeState],
	refreshInterval time.Duration,
) (Resolver[state.BlockNodeState, models.BlockNodeInputs], error) {
	br := &BlockNodeRuntimeResolver{
		state:           &blockNodeState,
		refreshInterval: refreshInterval,
		realityChecker:  realityChecker,
	}

	var err error

	br.namespace, err = NewEffectiveValue(cfg.BlockNode.Namespace, &validatedStringResolver{fieldName: "namespace"})
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create namespace resolver")
	}

	br.releaseName, err = NewEffectiveValue(cfg.BlockNode.Release, &validatedStringResolver{fieldName: "release name"})
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create release name resolver")
	}

	br.chartName, err = NewEffectiveValue(cfg.BlockNode.ChartName, &validatedStringResolver{fieldName: "chart name"})
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create chart name resolver")
	}

	br.chartRef, err = NewEffectiveValue(cfg.BlockNode.Chart, &chartRefResolver{})
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create chart ref resolver")
	}

	br.chartVersion, err = NewEffectiveValue(cfg.BlockNode.ChartVersion, &chartVersionResolver{intent: &br.intent})
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create chart version resolver")
	}

	br.storage, err = NewEffectiveValue(cfg.BlockNode.Storage, &storageResolver{})
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create storage resolver")
	}

	// Seed StrategyConfig from the provided config (StrategyDefault was seeded
	// above via NewEffectiveValueResolver's defaultVal).
	br.WithConfig(cfg)

	// Seed StrategyState from the initial persisted state.
	br.WithState(blockNodeState)

	return br, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// setOrClearString sets StrategyUserInput if val is non-empty, clears it otherwise.
func setOrClearString(r *EffectiveValue[string], strategy automa.EffectiveStrategy, val string) {
	if val != "" {
		_ = r.SetSource(strategy, val)
	} else {
		r.ClearSource(strategy)
	}
}
