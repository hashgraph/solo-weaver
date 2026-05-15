# Review guide — #587 catalog default field

> Phase 1 (#587) of the catalog redesign. Related phases: #588 (rename to
> `infrastructure-versions.yaml` + `host:`/`cluster:` sections) · #589 (wire
> Helm workflow steps + retire `deps.go` chart versions).

## Problem

`pkg/software/artifact.yaml` had no `default:` field. When an entry listed
multiple versions, `ArtifactMetadata.GetLatestVersion()` sorted them by
semver and returned the highest (`pkg/software/config.go`, called from
`pkg/software/base_installer.go:94`). The two consequences:

- Adding a new version entry — for testing, staging a future bump, or
  pinning a fix — silently became the new default for every operator.
- A reader of `artifact.yaml` could not tell which version would actually
  be selected without reading Go code.

## Solution

- Add `default: "<version>"` to every entry in `artifact.yaml`. All current
  entries list exactly one version, so the default matches what
  `GetLatestVersion()` returned before.
- Add a `Default Version` field on `ArtifactMetadata` and a new
  `GetDefaultVersion()` method that validates the field points to a known
  version and returns it. Error when the field is missing or names an
  unknown version.
- Replace `GetLatestVersion()` entirely; delete the now-unused
  `compareVersions` helper. `isValidVersion()` stays — it is still used by
  the `Test_Config_VersionsAreSemanticVersions` integration test.
- Switch the binary installer (`pkg/software/base_installer.go:94`) and all
  tests off `GetLatestVersion()` and onto `GetDefaultVersion()`.

## Files changed

| File | Description |
|---|---|
| `pkg/software/artifact.yaml` | Adds `default:` to all 8 entries; updates the header comment to explain the new field. |
| `pkg/software/config.go` | Adds `Default Version` field on `ArtifactMetadata`; replaces `GetLatestVersion()` with `GetDefaultVersion()`; removes `compareVersions`. |
| `pkg/software/base_installer.go` | Calls `GetDefaultVersion()` instead of `GetLatestVersion()` when the installer has no explicit version requested. |
| `pkg/software/config_test.go` | Rewrites `Test_Config_GetLatestVersion` as `Test_Config_GetDefaultVersion`; updates `Test_Config_VersionSelection` to assert default-based selection (1.9.0 wins over higher-semver 1.10.0 when explicitly defaulted). |
| `pkg/software/config_it_test.go` | Renames `Test_Config_GetLatestVersion_Integration` → `Test_Config_GetDefaultVersion_Integration` and now asserts every artifact declares an explicit `Default`; switches the remaining integration tests to `GetDefaultVersion()`. |

## Code review checklist

- [ ] Every entry in `pkg/software/artifact.yaml` has a `default:` field whose value matches a key under that entry's `versions:` map.
- [ ] The `default:` value for each entry matches what `GetLatestVersion()` would have returned before this PR (no behaviour change at install time).
- [ ] `ArtifactMetadata.Default` carries the `yaml:"default"` tag and uses the `Version` type so it round-trips through the loader.
- [ ] `GetDefaultVersion()` returns an error when `Default` is unset, and when `Default` names a version not present in `Versions`.
- [ ] No production code path still calls `GetLatestVersion()` — `grep -rn "GetLatestVersion" --include="*.go"` returns nothing.
- [ ] `compareVersions` is gone; `isValidVersion` and the `pkg/semver` import remain (still used by `Test_Config_VersionsAreSemanticVersions`).
- [ ] The new unit-test case "default selects one entry among many - not the highest semver" actually fails before the production change and passes after — confirms `default:` is what drives selection.
- [ ] `Test_Config_GetDefaultVersion_Integration` asserts `require.NotEmpty(t, artifact.Default, ...)` so a future entry added without `default:` will fail CI.
- [ ] The `artifact.yaml` header comment correctly describes the new contract: adding a version entry does not change the default; updating `default:` does.
- [ ] No structural rename slipped in — `artifact:` top-level key and `artifact.yaml` filename are unchanged (those belong to #588).

## Test commands

Unit tests (run on macOS host or VM):

```bash
go test -tags='!integration' -race -v -run 'Test_Config_' ./pkg/software/
```

Full unit suite for the package (Linux-only tests are skipped on macOS;
run the full suite via the VM):

```bash
task test:unit                            # current platform
task vm:test:unit                         # inside UTM VM (Linux)
```

Integration tests for `pkg/software` (require the VM):

```bash
task vm:test:integration TEST_NAME='^Test_Config_'
```

Lint check:

```bash
task lint:check
```

Expected: all `Test_Config_*` tests pass. The pre-existing
`Test_BaseInstaller_*` failures on macOS (caused by attempts to `mkdir /opt/solo`
and `internal/mount` being Linux-only) are unrelated and will only be exercised
in the VM.

## Manual UAT

These steps verify the new contract end-to-end.

### 1. Default selection drives installs

```bash
task build:weaver GOOS=linux GOARCH=arm64   # or your target arch
```

Confirm the binary builds without referencing `GetLatestVersion`:

```bash
go tool nm bin/solo-provisioner-linux-arm64 | grep -i GetLatestVersion
```

Expected: **no output**.

```bash
go tool nm bin/solo-provisioner-linux-arm64 | grep -i GetDefaultVersion
```

Expected: a single line referencing `software.(*ArtifactMetadata).GetDefaultVersion`.

### 2. Default validation rejects malformed entries

Edit `pkg/software/artifact.yaml`, delete the `default: "1.33.4"` line from
the `cri-o` entry, save, and run:

```bash
go test -tags='!integration' -race -run 'Test_Config_GetDefaultVersion_Integration/cri-o' ./pkg/software/
```

Expected output snippet:

```
    config_it_test.go:NN: Artifact cri-o must declare an explicit default version
--- FAIL: Test_Config_GetDefaultVersion_Integration/cri-o (0.00s)
```

Restore the `default:` line.

### 3. Default pointing to unknown version is rejected

Edit `pkg/software/artifact.yaml`, change `default: "1.33.4"` on `cri-o`
to `default: "9.9.9"`, save, and run:

```bash
go test -tags='!integration' -race -run 'Test_Config_GetDefaultVersion_Integration/cri-o' ./pkg/software/
```

Expected: failure with a `version 9.9.9 not found for cri-o` error coming
through `NewVersionNotFoundError`. Restore the `default:` line.

### 4. Adding a version entry does not change behaviour

Add a fake new version entry to `pkg/software/artifact.yaml` under any
component (e.g. `cri-o`'s `versions:`) without touching `default:`. Run:

```bash
go test -tags='!integration' -race -run 'Test_Config_GetDefaultVersion' ./pkg/software/
```

Expected: still passes. The default is still the originally-declared
version, not the higher semver of the fake entry. Revert the change.

### 5. Behaviour parity with main

For each artifact, confirm `GetDefaultVersion()` returns the same string
`GetLatestVersion()` returned on main:

```bash
go test -tags='!integration' -race -v -run 'Test_Config_GetDefaultVersion_Integration' ./pkg/software/
```

Expected versions per artifact:

| Artifact | Default |
|---|---|
| cri-o | `1.33.4` |
| kubelet | `1.33.4` |
| kubeadm | `1.33.4` |
| kubectl | `1.33.4` |
| helm | `3.18.6` |
| k9s | `0.50.9` |
| cilium | `0.18.7` |
| teleport | `18.6.4` |

These match the values `GetLatestVersion()` returned before this PR — no
operator-visible change at install time.
