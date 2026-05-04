# Plan: Two-User Security Model + hedera Directory Ownership (#394)

## Problem

Every file and directory created by `solo-provisioner` under `/opt/solo/weaver` was owned by
`root:root`. Block-node Helm init containers run as UID 2000 (`hedera`) and need write access
to their storage directories. The provisioner daemon should run as a non-root user (`weaver:2500`)
via passwordless sudo rather than as root.

---

## Design: Two identities + setgid directories

### Identities

| Identity | User | UID | Purpose |
|----------|------|-----|---------|
| **Process user** | `weaver` | 2500 | Runs the `solo-provisioner` daemon via systemd |
| **Storage owner** | `hedera` | 2000 | Owns block-node storage dirs; matches UID in block-node Helm init containers |

### Key insight: setgid directories, not runtime chown

A non-root process (`weaver:2500`) cannot call `syscall.Chown` to transfer ownership to another
user — that requires root or `CAP_CHOWN`. Instead:

1. **`weaver` is added to the `hedera` group** at install time (`usermod -aG hedera weaver`).
2. **Block-node storage directories** are created with owner `hedera:hedera` and mode `2775`
   (setgid + group-writable) — done once at install time, running as root.
3. Files the daemon writes into those dirs **inherit the `hedera` group automatically** via
   the setgid bit — no explicit `chown` call needed at runtime.
4. The block-node container (UID 2000) accesses via **owner** bits; the daemon (weaver, in the
   hedera group) accesses via **group** bits.

Weaver home directories (configs, binaries) use `weaver:weaver 0755` — no container access.

### Directory permission matrix

| Path | Owner | Mode | Rationale |
|------|-------|------|-----------|
| Block-node storage dirs | `hedera:hedera` | `2775` | Container write (owner) + daemon write (group via setgid) |
| Weaver home dirs (`/opt/solo/weaver/`) | `weaver:weaver` | `0755` | Daemon config — no container access |
| Rendered/temp config files | weaver-owned | `0644` | Read-only by containers via `other` bits |

### `fsx.Manager.WriteFile` — no ownership applied

`WriteFile` is a general-purpose method used by software installers that have nothing to do
with block-node storage. It must **not** apply any ownership. The private
`setupAccessForStorageOwner` helper is removed; `WriteFile` just writes bytes and returns.

---

## Implementation

### 1. Separate storage owner identity — `pkg/config/storage_owner.go` (new)

Hardcode `hedera:2000` as a non-configurable storage owner identity, separate from the
configurable `weaver:2500` service account in `pkg/config/svc_acc.go`.

Wire `security.SetStorageOwner(StorageOwner())` in `pkg/config/init.go`.

### 2. Block-node storage dir mode: 0755 → 2775

Add `DefaultStorageDirPerm = os.FileMode(0o2775)` to `pkg/models/paths.go`.

In `internal/blocknode/storage.go:SetupStorage` and `internal/blocknode/migration_storage.go:Execute`,
use `DefaultStorageDirPerm` instead of `DefaultDirOrExecPerm`.

### 3. Add `WriteOwnerByName` to `fsx.Manager`

`blocknode.Manager` has no `principal.Manager` field. Add `WriteOwnerByName(path, userName,
groupName string, recursive bool) error` to the `fsx.Manager` interface to consolidate the
lookup + `WriteOwner` pattern without requiring callers to hold a `principal.Manager`.

Call sites:
- `SetupStorage` / `StorageMigration.Execute`: `WriteOwnerByName(StorageOwnerUserName(), StorageOwnerGroupName(), true)`
- `SetupHomeDirectoryStructure`: `WriteOwnerByName(ServiceAccountUserName(), ServiceAccountGroupName(), true)`

### 4. Add weaver to hedera group at install time

In `internal/workflows/preflight.go:CheckWeaverUserStep`, after validating both users exist,
run `usermod -aG hedera weaver`. This is idempotent.

### 5. Remove `setupAccessForStorageOwner` from `WriteFile`

Delete the private helper. `WriteFile` returns nil after writing — no ownership applied.

### 6. Sudoers + systemd service

Add `internal/templates/files/weaver/sudoers` (passwordless sudo for weaver on specific
binaries) and `internal/templates/files/weaver/solo-provisioner.service` (systemd unit
running as weaver:weaver). Wire `InstallSudoersStep` and `InstallWeaverServiceStep` into
the self-install workflow.

---

## Files changed

| File | Change |
|------|--------|
| `pkg/security/security.go` | Add `StorageOwner` struct and `storageOwner*` accessors |
| `pkg/config/storage_owner.go` | **New** — hardcodes hedera:2000 |
| `pkg/config/init.go` | Wire `SetStorageOwner` |
| `pkg/models/paths.go` | Add `DefaultStorageDirPerm = 0o2775` |
| `pkg/fsx/manager_unix.go` | Add `WriteOwnerByName`; remove `setupAccessForStorageOwner`; `WriteFile` returns nil |
| `pkg/fsx/manager_unix_test.go` | Remove stale ownership assertions |
| `internal/blocknode/storage.go` | Use `WriteOwnerByName(hedera)` + `DefaultStorageDirPerm` |
| `internal/blocknode/migration_storage.go` | Same |
| `internal/workflows/steps/step_setup_directories.go` | Use `WriteOwnerByName(weaver)` |
| `internal/workflows/preflight.go` | Validate both users; run `usermod -aG hedera weaver` |
| `internal/workflows/steps/step_weaver.go` | Sudoers + service steps |
| `internal/workflows/weaver.go` | Wire new steps |
| `internal/templates/files/weaver/sudoers` | **New** |
| `internal/templates/files/weaver/solo-provisioner.service` | **New** |
| `docs/dev/security-model.md` | **New** — full design documentation |

---

## Acceptance criteria

- [ ] `id weaver` shows hedera in supplementary groups after install.
- [ ] Block-node storage dirs have mode `2775` and owner `hedera:hedera` after fresh install.
- [ ] File written by weaver into storage dir has group `hedera` (setgid inheritance).
- [ ] Preflight fails with clear message when hedera user is missing.
- [ ] `task lint` and `task vm:test:unit` pass.
