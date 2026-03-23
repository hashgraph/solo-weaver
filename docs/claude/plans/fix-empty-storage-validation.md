# Fix: BlockNodeStorage validation fails when basePath is in config but validated too early

## Context

When running `solo-provisioner block node install` with a config file, the command fails with:
```
common.illegal_state: invalid configuration, cause: common.illegal_argument:
either basePath must be provided or all of archivePath, livePath, and logPath must be provided
```

Despite `basePath` being correctly set (visible in the user inputs log as `/mnt/fast-storage/block-node`), the error occurs because `initializeDependencies()` validates the **raw config from the YAML file** before CLI flag values are merged.

## Root Cause Analysis

**Call flow in `install.go` RunE (lines 17-26):**
1. `prepareBlocknodeInputs(cmd, args)` — extracts root flags, calls `config.Initialize(path)` to load YAML, builds user inputs from CLI flags (with config fallback), logs inputs. **This succeeds.**
2. `initializeDependencies()` — calls `config.Get().Validate()` on the raw config. **This fails.**

**The bug in `config.Initialize()` (`pkg/config/global.go:58-77`):**
- When a config path is provided, it resets `globalConfig = models.Config{}` (line 60) — wiping all defaults including `BasePath: deps.BLOCK_NODE_STORAGE_BASE_PATH`
- Then it unmarshals ONLY what's in the YAML file
- If the YAML config doesn't explicitly set `blockNode.storage.basePath` (relying on CLI `--base-path` flag instead), the storage section ends up with all empty paths

**The problem in `BlockNodeConfig.Validate()` (`pkg/models/config.go:215-218`):**
- It unconditionally calls `c.Storage.Validate()`, which requires either `basePath` OR all of `archivePath`+`livePath`+`logPath`
- When storage is empty in the config (paths provided via CLI flags), this fails prematurely
- `BlockNodeStorage.Validate()` itself is correct — it should always enforce path requirements
- The bug is at the caller level: `BlockNodeConfig.Validate()` should skip storage validation when storage is empty

## Fix

Modify `BlockNodeConfig.Validate()` in `pkg/models/config.go` to skip `Storage.Validate()` when the storage section is empty. This is safe because:
- `BlockNodeInputs.Validate()` (in `inputs.go:149`) also calls `Storage.Validate()` AFTER CLI flags are merged
- An empty storage section in the config means paths will come from CLI flags
- `BlockNodeStorage.Validate()` remains unchanged — it still enforces path requirements when called directly

### File: `pkg/models/config.go` (lines 215-218)

Change:
```go
	// Validate storage paths
	if err := c.Storage.Validate(); err != nil {
		return err
	}
```

To:
```go
	// Validate storage paths only when storage is configured in config.
	// When empty, paths will be supplied via CLI flags and validated in BlockNodeInputs.Validate().
	if !c.Storage.IsEmpty() {
		if err := c.Storage.Validate(); err != nil {
			return err
		}
	}
```

### File: `pkg/models/validation_test.go` — update test

Change the `invalid_empty_optional_fields` test case in `TestBlockNodeConfig_Validate` to expect success since empty storage should be allowed at the config level:

```go
{
    name:        "valid_empty_storage",
    config:      BlockNodeConfig{
        Storage: BlockNodeStorage{},
    },
    expectError: false,
},
```

The `TestBlockNodeStorage_Validate` tests remain unchanged — `empty_all_paths` correctly expects an error when `Validate()` is called directly on empty storage.

### File: `pkg/models/validation_test.go` — add edge case tests to `TestBlockNodeConfig_Validate`

Added test cases to cover the new behavior and edge cases:

1. **Sizes without paths** — storage is NOT empty (has sizes), so validation runs and fails because no paths are provided
2. **All individual paths without basePath** — valid alternative to basePath
3. **Partial individual paths** — incomplete path set correctly fails
4. **Both base and individual paths** — providing both is valid
5. **Individual paths outside base hierarchy** — paths on different mount points are valid (no requirement that individual paths be under basePath)
6. **Base path with sizes only** — valid; individual paths derived from basePath at runtime
7. **Individual paths with sizes** — full explicit configuration without basePath
