# Teleport BLL Handler Refactor (GH #395)

## Context

The teleport commands (node install, cluster install) bypass the BLL handler pattern used by block node. They call workflows directly from the command layer and never persist software state to disk. This means `IsInstalled()` returns false even after installation, breaking uninstall detection.

The fix: refactor teleport to use the same BLL handler pattern as block node — command → handler → workflow → state flush.

## Scope

**Teleport has two distinct targets:**
1. **Node agent** (binary-based) — installs binaries, configures systemd, uses `state.MachineState.Software` for tracking
2. **Cluster agent** (Helm-based) — installs via Helm chart, uses `helm.IsInstalled()` for tracking

Both need BLL handlers. The cluster agent is simpler since Helm tracks its own release state, but we still need the handler pattern for consistency and action history.

## Architecture

```
cmd/weaver/commands/teleport/{node,cluster}/
    ├── init.go          (initializeDependencies → HandlerRegistry)
    ├── install.go       (intent → handler.HandleIntent)
    └── uninstall.go     (intent → handler.HandleIntent)
         ↓
internal/bll/teleport/
    ├── handler.go       (HandlerRegistry, ForAction)
    ├── node_install_handler.go
    ├── node_uninstall_handler.go
    ├── cluster_install_handler.go
    └── cluster_uninstall_handler.go
         ↓
internal/rsl/teleport_runtime.go   (TeleportRuntimeResolver)
internal/reality/teleport_checker.go (TeleportChecker)
internal/state/state.go            (TeleportState in StateRecord)
```

## Changes by layer (bottom-up)

### 1. Models — `pkg/models/`

**`intent.go`** — Add `TargetTeleportNode` and `TargetTeleportCluster` target types, add to `allowedOperations`

**`inputs.go`** — Add `TeleportNodeInputs` and `TeleportClusterInputs` structs with `Validate()`:
```go
type TeleportNodeInputs struct {
    Token     string
    ProxyAddr string
}

type TeleportClusterInputs struct {
    Version    string
    ValuesFile string
}
```

### 2. State — `internal/state/`

**`state.go`** — Add `TeleportState` to `StateRecord`:
```go
type StateRecord struct {
    // ... existing fields ...
    TeleportState TeleportState `yaml:"teleportState" json:"teleportState"`
}
```

Add `TeleportState` struct:
```go
type TeleportState struct {
    NodeAgent    TeleportNodeAgentState    `yaml:"nodeAgent" json:"nodeAgent"`
    ClusterAgent TeleportClusterAgentState `yaml:"clusterAgent" json:"clusterAgent"`
    LastSync     htime.Time                `yaml:"lastSync,omitempty" json:"lastSync,omitempty"`
}

type TeleportNodeAgentState struct {
    Installed  bool   `yaml:"installed" json:"installed"`
    Configured bool   `yaml:"configured" json:"configured"`
    Version    string `yaml:"version,omitempty" json:"version,omitempty"`
}

type TeleportClusterAgentState struct {
    Installed    bool   `yaml:"installed" json:"installed"`
    Release      string `yaml:"release,omitempty" json:"release,omitempty"`
    Namespace    string `yaml:"namespace,omitempty" json:"namespace,omitempty"`
    ChartVersion string `yaml:"chartVersion,omitempty" json:"chartVersion,omitempty"`
}
```

Update `Hashable()` to zero `TeleportState.LastSync`.

### 3. Reality — `internal/reality/`

**`teleport_checker.go`** (NEW) — Implements `Checker[state.TeleportState]`:
- `RefreshState()`: Check node agent via `software.NewTeleportNodeAgentInstaller().VerifyInstallation()`, check cluster agent via `helm.IsInstalled(deps.TELEPORT_RELEASE, deps.TELEPORT_NAMESPACE)`
- `FlushState()`: Atomically update the full state with new TeleportState

**`reality.go`** — Add `Teleport Checker[state.TeleportState]` to `Checkers` struct and `NewCheckers()`.

### 4. RSL — `internal/rsl/`

**`teleport_runtime.go`** (NEW) — `TeleportRuntimeResolver` implementing `Resolver[state.TeleportState, models.TeleportNodeInputs]`:
- Simpler than block node — teleport doesn't need effective value resolution (no storage paths, no chart overrides beyond what's in config)
- `RefreshState()` delegates to reality checker
- `CurrentState()` returns current TeleportState

**`runtime.go`** — Add `TeleportRuntime` to `RuntimeResolver`, wire into `NewRuntimeResolver()`, `Refresh()`, `CurrentState()`, `FlushAll()`.

### 5. BLL — `internal/bll/teleport/` (NEW package)

**`handler.go`** — `HandlerRegistry` with `ForNodeAction()`/`ForClusterAction()` dispatching to per-action handlers

**`node_install_handler.go`** — `NodeInstallHandler`:
- `PrepareEffectiveInputs()`: pass through (no resolution needed)
- `BuildWorkflow()`: returns `steps.SetupTeleportNodeAgent(sm)`

**`node_uninstall_handler.go`** — `NodeUninstallHandler`:
- `BuildWorkflow()`: returns `steps.TeardownTeleportNodeAgent(sm)`

**`cluster_install_handler.go`** — `ClusterInstallHandler`:
- `PrepareEffectiveInputs()`: pass through
- `BuildWorkflow()`: returns `steps.SetupTeleportClusterAgent()`

**`cluster_uninstall_handler.go`** — `ClusterUninstallHandler`:
- `BuildWorkflow()`: returns `steps.TeardownTeleportClusterAgent()`

### 6. Commands — `cmd/weaver/commands/teleport/`

**`node/init.go`** (NEW) — `initializeDependencies()` creating StateManager → RealityCheckers → RuntimeResolver → HandlerRegistry

**`node/install.go`** — Rewrite to use handler pattern:
```go
err := initializeDependencies()
intent := models.Intent{Action: ActionInstall, Target: TargetTeleportNode}
inputs := &models.UserInputs[models.TeleportNodeInputs]{...}
handler, _ := teleportHandler.ForNodeAction(intent.Action)
common.RunWorkflow(ctx, func() { return handler.HandleIntent(ctx, intent, *inputs) })
```

**`node/uninstall.go`** — Same pattern with `ActionUninstall`

**`cluster/init.go`** (NEW) — Same initialization pattern

**`cluster/install.go`** — Rewrite to use handler pattern

**`cluster/uninstall.go`** — Rewrite to use handler

### 7. BaseHandler fixes

- **`BaseHandler.HandleIntent`** had `models.TargetBlockNode` hardcoded at line 87 — changed to use `h.Target` field, set via `NewBaseHandler(runtime, target...)`
- **`CommonInputs.ExecutionOptions`** must be initialized with `StopOnError`/`ContinueOnError` defaults — empty struct fails validation

## Files summary

| File | Action |
|------|--------|
| `pkg/models/intent.go` | Add `TargetTeleportNode`, `TargetTeleportCluster` |
| `pkg/models/inputs.go` | Add `TeleportNodeInputs`, `TeleportClusterInputs` |
| `internal/state/state.go` | Add `TeleportState` to `StateRecord`, update `Hashable()` |
| `internal/reality/teleport_checker.go` | **NEW** |
| `internal/reality/reality.go` | Add `Teleport` to `Checkers` |
| `internal/rsl/teleport_runtime.go` | **NEW** |
| `internal/rsl/runtime.go` | Add `TeleportRuntime` field |
| `internal/bll/base_handler.go` | Make target configurable via `Target` field |
| `internal/bll/teleport/handler.go` | **NEW** |
| `internal/bll/teleport/node_install_handler.go` | **NEW** |
| `internal/bll/teleport/node_uninstall_handler.go` | **NEW** |
| `internal/bll/teleport/cluster_install_handler.go` | **NEW** |
| `internal/bll/teleport/cluster_uninstall_handler.go` | **NEW** |
| `cmd/weaver/commands/teleport/node/init.go` | **NEW** |
| `cmd/weaver/commands/teleport/node/install.go` | Rewrite to use handler |
| `cmd/weaver/commands/teleport/node/uninstall.go` | Rewrite to use handler |
| `cmd/weaver/commands/teleport/node/node.go` | Cleanup |
| `cmd/weaver/commands/teleport/cluster/init.go` | **NEW** |
| `cmd/weaver/commands/teleport/cluster/install.go` | Rewrite to use handler |
| `cmd/weaver/commands/teleport/cluster/uninstall.go` | Rewrite to use handler |
