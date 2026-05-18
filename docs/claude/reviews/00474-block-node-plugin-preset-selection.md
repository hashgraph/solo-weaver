# Review Guide: 00474 — Block Node Plugin Preset Selection

## Summary

- Added interactive TUI prompts (preset select + custom multi-select) and `--plugin-preset` / `--plugins` flags so node operators can choose which block-node plugins are deployed at install, upgrade, and reconfigure time.
- The preset-to-plugin mapping is owned by solo-weaver (`internal/blocknode/blocknode_plugins.go`) and must be updated in lockstep with block-node chart releases.
- The resolved plugin list is injected into `plugins.names` in the Helm values file, replacing the previously hardcoded chart default.

## Changed Files

| File | Change |
|------|--------|
| `internal/blocknode/blocknode_plugins.go` | **New** — preset IDs, plugin catalog for multi-select, canonical plugin lists, helper functions |
| `internal/blocknode/blocknode_plugins_test.go` | **New** — unit tests for preset helpers and `injectPluginsConfig` |
| `internal/blocknode/values.go` | Added `injectPluginsConfig()` method; wired into `ComputeValuesFile` pipeline |
| `internal/ui/prompt/blocknode.go` | Added `RunPluginPresetPrompts` (two-pass: select + conditional multi-select) |
| `cmd/weaver/commands/block/node/node.go` | Added `flagPluginPreset` / `flagPlugins` vars and persistent flags |
| `cmd/weaver/commands/block/node/init.go` | Wired prompt call; resolved plugin list and populated `PluginPreset`/`PluginList` in inputs |
| `pkg/models/inputs.go` | Added `PluginPreset`, `PluginList` fields + `DefaultPluginPreset`, `PluginPresetCustom` constants + `ValidatePluginList` |
| `internal/bll/blocknode/helpers.go` | Pass-through for `PluginPreset`/`PluginList`; persistence in `patchBlockNodeState` |
| `internal/state/state.go` | Added `PluginPreset`, `PluginList` to `BlockNodeState` |
| `internal/state/state_reader.go` | Added `PluginPreset`/`PluginList` to `PromptDefaultsDoc`, `BlockNodeSummary`, `ReadPromptDefaultsFromDisk` |
| `internal/state/state_reader_test.go` | Extended `TestReadPromptDefaults_ParsesAllBlockNodeFields` to cover new fields |

## Code Review Checklist

- [ ] `injectPluginsConfig` is called AFTER `injectRetentionConfig` and BEFORE `injectServiceAnnotations` in `ComputeValuesFile`
- [ ] `--plugins` takes precedence over `--plugin-preset` in `prepareBlocknodeInputs` (see `pluginList := flagPlugins` before preset lookup)
- [ ] `RunPluginPresetPrompts` is a no-op when either flag was already set on the command line (`flagWasSet` checks)
- [ ] The custom multi-select pre-fills from the last-used `PluginList` persisted in state (reconfigure default)
- [ ] `patchBlockNodeState` persists `PluginPreset` and `PluginList` only when `PluginPreset != ""` (no spurious state writes)
- [ ] `ValidatePluginList` rejects empty entries and entries with surrounding whitespace
- [ ] `blocknode_plugins.go` lists both Tier 1 presets; Tier 2 presets are not yet included (pending block-node team confirmation)
- [ ] `AllBlockNodePlugins` contains all selectable plugins for the custom multi-select
- [ ] The `strings` import was added to `pkg/models/inputs.go` alongside the new validation function

## Tests

```bash
# Unit tests for blocknode plugins and values injection
go test -race -tags='!integration' -run 'TestAvailable|TestPluginList|TestPresetLabel|TestIsKnown|TestInjectPlugins' ./internal/blocknode/...

# State reader tests (including new plugin preset fields)
go test -race -tags='!integration' ./internal/state/...

# Full unit suite (macOS — excludes Linux-only packages)
go test -race -tags='!integration' ./internal/blocknode/... ./internal/state/... ./internal/ui/prompt/... ./pkg/models/...
```

## Manual UAT

### Prerequisites

A running block-node environment (kubeconfig, PostgreSQL, Helm).

### 1. Interactive install — preset select

```bash
sudo solo-provisioner block node install
```

Expected: after the storage path prompts, a `Block Node Plugin Preset` select appears with options:
- `Tier 1 — Local Full History  (blocks stored on local disk)`
- `Tier 1 — Remote Full History  (blocks stored in S3-compatible cloud storage)`
- `Custom  (select individual plugins)`

Select `Tier 1 — Local Full History` and complete the install.

Verify the deployed Helm values contain:
```yaml
plugins:
  names: facility-messaging,block-access-service,health,server-status,stream-publisher,stream-subscriber,verification,blocks-file-historic,blocks-file-recent,backfill
```

### 2. Interactive install — custom multi-select

```bash
sudo solo-provisioner block node install
```

Select `Custom`. A multi-select appears listing all available plugins.
Choose `facility-messaging` and `health` only.

Verify the deployed values contain:
```yaml
plugins:
  names: facility-messaging,health
```

### 3. Non-interactive — `--plugin-preset` flag

```bash
sudo solo-provisioner block node install --plugin-preset tier1-rfh --non-interactive  -p local
```

Expected: no prompt shown; deployed values contain the RFH plugin list including `s3-archive`.

### 4. Non-interactive — `--plugins` override

```bash
sudo solo-provisioner block node install --plugins "facility-messaging,health" --non-interactive  -p local
```

Expected: deployed values contain `plugins.names: facility-messaging,health`.

### 5. Reconfigure — last preset shown as default

After completing step 1 (Tier 1 LFH), run:

```bash
sudo solo-provisioner block node reconfigure
```

Expected: the plugin preset prompt pre-selects `Tier 1 — Local Full History`.

### 6. `--plugins` overrides `--plugin-preset`

```bash
sudo solo-provisioner block node install --plugin-preset tier1-lfh --plugins "facility-messaging,health" --non-interactive -p local
```

Expected: `--plugins` wins; deployed values contain `facility-messaging,health` (not the full LFH list).

### 7. Validation — empty plugin list or preset

```bash
sudo solo-provisioner block node install --plugins "" --non-interactive -p local
```

```bash
sudo solo-provisioner block node install --non-interactive
```

Expected: the block node installation succeeds as previous behaviour with the default set of plugins installed.
