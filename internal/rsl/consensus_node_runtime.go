// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"sync"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

// ConsensusNodeRuntimeResolver resolves the effective inputs for a single
// consensus node. It deliberately uses two mechanisms, matched to the field:
//
//   - The 4 identity scalars (imageRepo, imageTag, ledgerId, chainId) use
//     EffectiveValue[string]: they arbitrate between competing sources (deployed
//     Reality vs. CLI UserInput vs. deployment-package Config vs. Default) with
//     Reality winning, which is what gives a re-run its idempotency.
//   - The config-file contents use a plain deployment-package-over-embedded
//     coalesce (ResolveConfigContents). They have no Reality/State layer and no
//     validation, so the EffectiveValue machinery would be pure ceremony. See
//     docs/dev/building-a-node-feature.md, "When to use EffectiveValue".
//
// The scalar priority order is Reality > State > UserInput > Config (deployment
// package) > Default > Zero. Consensus node upgrades use a separate protocol,
// not individual commands.
type ConsensusNodeRuntimeResolver struct {
	mu              sync.Mutex
	nodes           map[string]state.ConsensusNodeState
	persistedNodes  map[string]state.ConsensusNodeState // from state.yaml, survives RefreshState
	refreshInterval time.Duration
	realityChecker  reality.Checker[map[string]state.ConsensusNodeState]
	intent          *models.Intent
	lastRefresh     htime.Time

	// Identity scalars — arbitrated via EffectiveValue (Reality wins).
	imageRepo *EffectiveValue[string]
	imageTag  *EffectiveValue[string]
	ledgerId  *EffectiveValue[string]
	chainId   *EffectiveValue[string]
}

func (r *ConsensusNodeRuntimeResolver) WithIntent(intent models.Intent) Resolver[map[string]state.ConsensusNodeState, models.ConsensusNodeInputs] {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.intent = &intent
	return r
}

func (r *ConsensusNodeRuntimeResolver) WithUserInputs(inputs models.ConsensusNodeInputs) Resolver[map[string]state.ConsensusNodeState, models.ConsensusNodeInputs] {
	r.mu.Lock()
	defer r.mu.Unlock()

	setOrClearString(r.imageRepo, StrategyUserInput, inputs.ConsensusImageRepo)
	setOrClearString(r.imageTag, StrategyUserInput, inputs.ConsensusImageTag)
	setOrClearString(r.ledgerId, StrategyUserInput, inputs.LedgerId)
	setOrClearString(r.chainId, StrategyUserInput, inputs.ChainId)

	return r
}

// WithDeploymentPackage extracts the identity scalars from the deployment
// package and sets them as StrategyConfig sources. StrategyConfig is repurposed
// here for the deployment package (not config.yaml) since it occupies the right
// priority slot: below UserInput, above Default. Config-file contents are
// resolved separately by ResolveConfigContents.
func (r *ConsensusNodeRuntimeResolver) WithDeploymentPackage(pkgDir string, nodeId int64) Resolver[map[string]state.ConsensusNodeState, models.ConsensusNodeInputs] {
	if pkgDir == "" {
		return r
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	l := logx.As()

	if repo, tag, err := resolveImageFromManifest(pkgDir); err == nil {
		setOrClearString(r.imageRepo, StrategyConfig, repo)
		setOrClearString(r.imageTag, StrategyConfig, tag)
	} else {
		l.Warn().Err(err).Msg("Failed to resolve image from deployment package manifest")
	}

	if lid, cid, err := resolveLedgerAndChain(pkgDir); err == nil {
		setOrClearString(r.ledgerId, StrategyConfig, lid)
		setOrClearString(r.chainId, StrategyConfig, cid)
	} else {
		l.Warn().Err(err).Msg("Failed to resolve ledger/chain from deployment package")
	}

	return r
}

// consensusConfigFile maps one config file to its stable key, its
// deployment-package path (per-node for block-nodes), its embedded-template
// fallback, and the inputs field it populates. consensusConfigFiles is the
// single source of truth for the config-file set — the create-config-CR step
// and this resolver both derive from it.
type consensusConfigFile struct {
	key      string
	pkgPath  func(nodeId int64) string
	embedded string
	set      func(c *models.ConsensusNodeInputs, v string)
}

func consensusConfigFiles() []consensusConfigFile {
	fixed := func(p string) func(int64) string { return func(int64) string { return p } }
	return []consensusConfigFile{
		{models.ConfigKeyLog4j2, fixed("log4j2.xml"), "consensus/log4j2.xml", func(c *models.ConsensusNodeInputs, v string) { c.ConfigLog4j2 = v }},
		{models.ConfigKeySettings, fixed("settings.txt"), "consensus/settings.txt", func(c *models.ConsensusNodeInputs, v string) { c.ConfigSettings = v }},
		{models.ConfigKeyAppProperties, fixed("data/config/application.properties"), "consensus/application.properties", func(c *models.ConsensusNodeInputs, v string) { c.ConfigAppProperties = v }},
		{models.ConfigKeyAppOverride, fixed("data/config/application-override.properties"), "consensus/application-override.properties", func(c *models.ConsensusNodeInputs, v string) { c.ConfigAppOverrideProperties = v }},
		{models.ConfigKeyApiPermission, fixed("data/config/api-permission.properties"), "consensus/api-permission.properties", func(c *models.ConsensusNodeInputs, v string) { c.ConfigApiPermission = v }},
		{models.ConfigKeyBootstrap, fixed("data/config/bootstrap.properties"), "consensus/bootstrap.properties", func(c *models.ConsensusNodeInputs, v string) { c.ConfigBootstrap = v }},
		{models.ConfigKeyNodeProperties, fixed("data/config/node.properties"), "consensus/node.properties", func(c *models.ConsensusNodeInputs, v string) { c.ConfigNodeProperties = v }},
		{models.ConfigKeyFeeSchedules, fixed("data/config/feeSchedules.json"), "consensus/feeSchedules.json", func(c *models.ConsensusNodeInputs, v string) { c.ConfigFeeSchedules = v }},
		{models.ConfigKeySimpleFeesSchedules, fixed("data/config/simpleFeesSchedules.json"), "consensus/simpleFeesSchedules.json", func(c *models.ConsensusNodeInputs, v string) { c.ConfigSimpleFeesSchedules = v }},
		{models.ConfigKeyThrottles, fixed("data/config/throttles.json"), "consensus/throttles.json", func(c *models.ConsensusNodeInputs, v string) { c.ConfigThrottles = v }},
		{models.ConfigKeyBlockNodes, blockNodesRelPath, "consensus/block-nodes.json", func(c *models.ConsensusNodeInputs, v string) { c.ConfigBlockNodes = v }},
	}
}

// ResolveConfigContents populates each config-file field on custom by coalescing
// the deployment-package copy over the embedded default: package content wins
// when present and non-empty, otherwise the embedded template is used. The
// per-file source is recorded in custom.ConfigSources so the apply step can tell
// an explicit (package) change from an implicit (embedded) one. It reads files
// only and mutates the caller's struct, so it needs no lock.
//
// There is no UserInput layer — config contents are not exposed as CLI flags. If
// that changes (or config drift detection adds a State/Reality layer), promote
// the relevant field to EffectiveValue then rather than carrying the machinery
// speculatively now.
func (r *ConsensusNodeRuntimeResolver) ResolveConfigContents(pkgDir string, nodeId int64, custom *models.ConsensusNodeInputs) {
	custom.ConfigSources = make(map[string]string, len(consensusConfigFiles()))
	for _, f := range consensusConfigFiles() {
		content := embeddedConfigContent(f.embedded)
		source := models.ConfigSourceEmbedded
		if pkgDir != "" {
			if pkg, err := readFileFromPackage(pkgDir, f.pkgPath(nodeId)); err == nil && pkg != "" {
				content = pkg
				source = models.ConfigSourcePackage
			}
		}
		f.set(custom, content)
		custom.ConfigSources[f.key] = source
	}
}

// embeddedConfigContent returns the embedded template body at files/<path>, or
// "" when it is absent.
func embeddedConfigContent(path string) string {
	if b, err := templates.Files.ReadFile("files/" + path); err == nil {
		return string(b)
	}
	return ""
}

func (r *ConsensusNodeRuntimeResolver) WithDefaults(_ models.Config) Resolver[map[string]state.ConsensusNodeState, models.ConsensusNodeInputs] {
	return r
}

func (r *ConsensusNodeRuntimeResolver) WithConfig(_ models.Config) Resolver[map[string]state.ConsensusNodeState, models.ConsensusNodeInputs] {
	return r
}

func (r *ConsensusNodeRuntimeResolver) WithEnv(_ models.Config) Resolver[map[string]state.ConsensusNodeState, models.ConsensusNodeInputs] {
	return r
}

func (r *ConsensusNodeRuntimeResolver) WithState(st map[string]state.ConsensusNodeState) Resolver[map[string]state.ConsensusNodeState, models.ConsensusNodeInputs] {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes = st
	return r
}

func (r *ConsensusNodeRuntimeResolver) SetStateForNode(scope string, ns state.ConsensusNodeState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	setOrClearString(r.imageRepo, StrategyState, ns.ImageRepo)
	setOrClearString(r.imageTag, StrategyState, ns.ImageTag)
	setOrClearString(r.ledgerId, StrategyState, ns.LedgerId)
	setOrClearString(r.chainId, StrategyState, ns.ChainId)
}

func (r *ConsensusNodeRuntimeResolver) SetRealityForNode(scope string, ns state.ConsensusNodeState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	setOrClearString(r.imageRepo, StrategyReality, ns.ImageRepo)
	setOrClearString(r.imageTag, StrategyReality, ns.ImageTag)
	setOrClearString(r.ledgerId, StrategyReality, ns.LedgerId)
	setOrClearString(r.chainId, StrategyReality, ns.ChainId)
}

// ── Field accessors ──────────────────────────────────────────────────────────
//
// The scalars all use DefaultSelector, whose Resolve never errors, so the
// accessors return the EffectiveValue directly. Required-field validation is
// done once in the handler after resolution. Give a field a validating Selector
// (and an error-returning accessor) only if it actually needs one.

func (r *ConsensusNodeRuntimeResolver) ImageRepo() *EffectiveValue[string] { return r.imageRepo }
func (r *ConsensusNodeRuntimeResolver) ImageTag() *EffectiveValue[string]  { return r.imageTag }
func (r *ConsensusNodeRuntimeResolver) LedgerId() *EffectiveValue[string]  { return r.ledgerId }
func (r *ConsensusNodeRuntimeResolver) ChainId() *EffectiveValue[string]   { return r.chainId }

// ── State refresh ────────────────────────────────────────────────────────────

func (r *ConsensusNodeRuntimeResolver) RefreshState(ctx context.Context, force bool) error {
	logx.As().Debug().Msg("Refreshing consensus node state using reality checker")

	now := htime.Now()
	if !force {
		r.mu.Lock()
		if now.Sub(r.lastRefresh) < r.refreshInterval {
			r.mu.Unlock()
			return nil
		}
		r.mu.Unlock()
	}

	nodes, err := r.realityChecker.RefreshState(ctx)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to refresh consensus node state")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes = nodes
	r.lastRefresh = now

	return nil
}

func (r *ConsensusNodeRuntimeResolver) CurrentState() (map[string]state.ConsensusNodeState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.nodes == nil {
		return make(map[string]state.ConsensusNodeState), nil
	}
	out := make(map[string]state.ConsensusNodeState, len(r.nodes))
	for k, v := range r.nodes {
		out[k] = v
	}
	return out, nil
}

// PersistedNodes returns the state as it was loaded from state.yaml at
// construction time, before any reality-checker refresh. Use this to seed
// the StrategyState layer so it is distinct from the StrategyReality layer.
func (r *ConsensusNodeRuntimeResolver) PersistedNodes() map[string]state.ConsensusNodeState {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.persistedNodes == nil {
		return make(map[string]state.ConsensusNodeState)
	}
	out := make(map[string]state.ConsensusNodeState, len(r.persistedNodes))
	for k, v := range r.persistedNodes {
		out[k] = v
	}
	return out
}

// ── Constructor ──────────────────────────────────────────────────────────────

func NewConsensusNodeRuntimeResolver(
	_ models.Config,
	nodes map[string]state.ConsensusNodeState,
	realityChecker reality.Checker[map[string]state.ConsensusNodeState],
	refreshInterval time.Duration,
) (Resolver[map[string]state.ConsensusNodeState, models.ConsensusNodeInputs], error) {
	persisted := make(map[string]state.ConsensusNodeState, len(nodes))
	for k, v := range nodes {
		persisted[k] = v
	}
	cr := &ConsensusNodeRuntimeResolver{
		nodes:           nodes,
		persistedNodes:  persisted,
		refreshInterval: refreshInterval,
		realityChecker:  realityChecker,
	}

	var err error
	newStr := func() (*EffectiveValue[string], error) {
		return NewEffectiveValue[string](&DefaultSelector[string]{})
	}

	if cr.imageRepo, err = newStr(); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create imageRepo resolver")
	}
	if cr.imageTag, err = newStr(); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create imageTag resolver")
	}
	if cr.ledgerId, err = newStr(); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create ledgerId resolver")
	}
	if cr.chainId, err = newStr(); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create chainId resolver")
	}

	return cr, nil
}
