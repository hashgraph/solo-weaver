# Review Guide: hedera Directory Ownership + System User Auto-Creation (#394)

## Summary

This PR fixes the ownership model for provisioner home directories, implements programmatic
auto-creation of the `weaver:2500` and `hedera:2000` system accounts (no manual pre-creation
required), and consolidates the split `ServiceAccount`/`HederaOwner` types into a single
`SystemUser` type with consistent `Weaver*`/`Hedera*` naming throughout the codebase.

### Problems addressed

1. **Sandbox dirs owned by hedera**: `SetupHomeDirectoryStructure` applied `hedera:hedera 2775`
   to every directory under `/opt/solo/weaver`, including sandbox dirs bind-mounted to system
   paths (e.g. `SandboxBinDir` -> `/hostbin` inside the cilium init container). Cilium's
   `mount-cgroup` init container (not in the hedera group) hit `Permission denied`.

2. **Wrong owner for provisioner home dirs**: `bin`, `logs`, `state`, etc. are written by the
   `weaver` service, not Hedera node software. The correct owner is `root:weaver 2775` with
   setgid so new files inherit the weaver group.

3. **setgid silently dropped**: Go's `os.Chmod(path, os.FileMode(0o2775))` does NOT set the
   kernel setgid bit. `0o2775 & (1 << 23) == 0` — the `02000` octal part is silently dropped.
   Fix: `fs.ModeSetgid | 0o775`.

4. **`state.yaml` mode 0600**: `atomicWriteFile` uses `os.CreateTemp` (always 0600), then
   renames without chmod. The weaver daemon (uid 2500) cannot read its own state file.

5. **`solo-provisioner.log` mode 0600**: lumberjack creates the log file with hardcoded 0600.
   The weaver daemon cannot write its own log file.

6. **`hedera:2000` a manual prerequisite**: `CreateGroupWithId`/`CreateUserWithId` in
   `pkg/security/principal` were panicking stubs. Operators had to create hedera before
   running `block node install`.

7. **`weaver:2500` a manual prerequisite**: Operators had to create weaver before running
   `solo-provisioner install`. The new `EnsureWeaverOwnerStep` auto-creates it.

8. **Stale type names**: `ServiceAccount` and `HederaOwner` types were inconsistently named;
   accessors used mismatched prefixes. Merged into `SystemUser` with consistent
   `Weaver*`/`Hedera*` accessors.

### Ownership model (final)

| Resource | Owner | Mode | How |
|----------|-------|------|-----|
| Provisioner home dirs (`bin`, `logs`, `state`, etc.) | `root:weaver` | `2775` (setgid) | `SetupHomeDirectoryStructure`, non-sandbox branch |
| Sandbox dirs (`/opt/solo/weaver/sandbox/...`) | `root:root` | `0755` | `SetupHomeDirectoryStructure`, sandbox branch |
| `state.yaml` | `root:weaver` | `0640` | Explicit `chmod` after atomic rename |
| `solo-provisioner.log` | `root:weaver` | `0640` | Pre-created before lumberjack init |
| Block-node storage (`live`, `archive`, `logs`, etc.) | `hedera:hedera` | `2775` (setgid) | `blocknode/storage.go` |

## Changed Files

| File | Change |
|------|--------|
| `pkg/models/paths.go` | `DefaultStorageDirPerm = fs.ModeSetgid | 0o775` -- fix silent setgid drop |
| `pkg/security/security.go` | Replace `ServiceAccount`+`HederaOwner` with `SystemUser`; `Weaver*`/`Hedera*` accessors |
| `pkg/security/security_test.go` | Update test names for new type/function names |
| `pkg/config/identities.go` | **New** -- merged replacement for `svc_acc.go` + `hedera_owner.go` |
| `pkg/config/svc_acc.go` | **Deleted** |
| `pkg/config/hedera_owner.go` | **Deleted** |
| `pkg/config/init.go` | `SetServiceAccount` -> `SetWeaverUser`; `SetHederaOwner` -> `SetHederaUser` |
| `pkg/config/global.go` | Same accessor renames |
| `pkg/security/principal/interface.go` | Add `WriteGroupEntry`, `WriteGroupShadowEntry`, `WriteUserEntry`, `WriteUserShadowEntry` to `Provider` interface |
| `pkg/security/principal/provider_utils_unix.go` | Add `appendToFile` / `appendToFileIfExists` write helpers |
| `pkg/security/principal/provider_unix.go` | Add shadow-file constants; implement all 4 `Write*` methods (merged from deleted `provider_linux.go`) |
| `pkg/security/principal/provider_darwin.go` | Stubs for all 4 write methods returning `errorx.NotSupported` |
| `pkg/security/principal/manager.go` | Remove 4 panicking stub methods |
| `pkg/security/principal/manager_create_stub.go` | **New** (`darwin || windows`) -- stubs returning not-supported |
| `pkg/security/principal/manager_create_unix.go` | **New** (`!darwin && !windows`) -- real `CreateGroupWithId`/`CreateUserWithId` delegating to `provider.Write*` then Refresh |
| `pkg/security/principal/mocks_generated.go` | Regenerated -- `MockProvider` includes 4 new write methods |
| `internal/workflows/steps/step_ensure_hedera_owner.go` | **New** -- `EnsureHederaOwnerStep`: auto-creates `hedera:2000`, validates GID/UID mismatch, runs `usermod -aG hedera weaver` |
| `internal/workflows/steps/step_ensure_weaver_owner.go` | **New** -- `EnsureWeaverOwnerStep`: auto-creates `weaver:2500` (nologin, no home dir) |
| `internal/workflows/steps/step_block_node.go` | Add `EnsureHederaOwnerStep` as first step in `SetupBlockNode` |
| `internal/workflows/steps/step_setup_directories.go` | Sandbox branch: `root:root 0755`; home branch: `root:weaver 2775` |
| `internal/workflows/weaver.go` | Add `EnsureWeaverOwnerStep` as second step in `NewSelfInstallWorkflow` |
| `internal/workflows/preflight.go` | Remove hedera:2000 validation block; keep only `weaver:2500` check |
| `internal/kube/admin.go` | Remove `configureWeaverKubeConfig` (nologin weaver has no home dir) |
| `internal/state/atomic_write.go` | Add `os.Chmod(path, 0o640)` after `os.Rename` |
| `cmd/weaver/commands/root.go` | Pre-create log file with `0o640` before lumberjack init; `ServiceAccountGroupId` -> `WeaverGroupId` |
| `internal/blocknode/storage.go` | `HederaOwnerUserName`/`HederaOwnerGroupName` -> `HederaUserName`/`HederaGroupName` |
| `internal/blocknode/migration_storage.go` | Same accessor renames |
| `internal/state/state_manager_test.go` | `ServiceAccount*` -> `Weaver*` |
| `.github/workflows/zxc-unit-test.yaml` | Remove manual `weaver`/`hedera` user creation steps |
| `.github/workflows/zxc-integration-test.yaml` | Same |
| `.github/workflows/zxc-uat-test.yaml` | Same |
| `taskfiles/vm.yaml` | Remove manual user creation block |
| `docs/quickstart.md` | Remove manual user prerequisites; note that both are auto-created |
| `internal/bll/blocknode/install_handler.go` | Add `EnsureWeaverOwnerStep` as first step in both install branches (before preflight) |
| `taskfiles/uat.yaml` | Restructure `uat:compat` to upgrade CLI before block-node install (fixes compat test failure) |

## Code Review Checklist

### setgid fix
- [ ] `pkg/models/paths.go`: `DefaultStorageDirPerm = fs.ModeSetgid | 0o775` (not `0o2775`)
- [ ] `io/fs` is imported in `paths.go`

### Directory ownership split
- [ ] `SetupHomeDirectoryStructure` builds a `sandboxSet` from `pp.SandboxDirectories` before the loop
- [ ] Sandbox branch: only `WritePermissions(dir, DefaultDirOrExecPerm, false)` -- no `WriteOwnerByName`
- [ ] Non-sandbox branch: `WritePermissions(dir, DefaultStorageDirPerm, false)` + `WriteOwnerByName(dir, "root", security.WeaverGroupName(), false)`
- [ ] Block-node storage (`blocknode/storage.go`) still sets `hedera:hedera` -- unchanged

### File mode fixes
- [ ] `atomic_write.go`: `os.Chmod(path, 0o640)` called after `os.Rename`, before `success = true`
- [ ] `root.go`: log file pre-created with `os.OpenFile(..., os.O_CREATE|os.O_WRONLY, 0o640)` before `logx.Initialize`

### Provider interface + write methods
- [ ] `Provider` interface in `interface.go` declares all 4 write methods
- [ ] `provider_unix.go` (`!darwin && !windows`): `WriteGroupEntry` appends `name:x:gid:\n` to `/etc/group`
- [ ] `provider_unix.go`: `WriteUserEntry` appends `name:x:uid:uid::/:/usr/sbin/nologin\n` to `/etc/passwd`
- [ ] `provider_unix.go`: `WriteGroupShadowEntry` and `WriteUserShadowEntry` use `appendToFileIfExists` (no error if file absent)
- [ ] `provider_darwin.go`: all 4 write methods return `errorx.NotSupported`
- [ ] `appendToFile` opens with `O_APPEND|O_WRONLY` -- never truncates existing file
- [ ] `appendToFileIfExists` returns `nil` (not error) when the file does not exist
- [ ] No `provider_linux.go` file exists -- code merged into `provider_unix.go`

### Manager Create methods
- [ ] `manager.go` no longer contains panicking stubs
- [ ] `manager_create_unix.go`: `CreateGroupWithId` calls `WriteGroupEntry`, then `WriteGroupShadowEntry` (best-effort), then `Refresh()`, then `LookupGroupByName`
- [ ] `manager_create_unix.go`: `CreateUserWithId` calls `WriteUserEntry`, then `WriteUserShadowEntry` (best-effort), then `Refresh()`, then `LookupUserByName`
- [ ] `manager_create_stub.go` (`darwin || windows`): all 4 methods return `errorx.NotSupported`
- [ ] Mocks regenerated -- `MockProvider` includes all 4 new write methods

### EnsureHederaOwnerStep
- [ ] Step ID is `"ensure-hedera-owner"`
- [ ] Group path: `LookupGroupByName` -> not found: `CreateGroupWithId`; found with wrong GID: fail with `groupmod` fix instructions
- [ ] User path: `LookupUserByName` -> not found: `CreateUserWithId`; found with wrong UID: fail with `usermod -u` fix instructions
- [ ] `usermod -aG hedera weaver` runs after group/user are ensured (idempotent)
- [ ] `usermod` failure logs a warning but does NOT hard-fail the step
- [ ] `EnsureHederaOwnerStep()` is the first step in `SetupBlockNode`, before `setupBlockNodeStorage`

### EnsureWeaverOwnerStep
- [ ] Step ID is `"ensure-weaver-owner"`
- [ ] Same GID/UID mismatch detection as EnsureHederaOwnerStep
- [ ] No `usermod -aG` at end (weaver doesn't need to join any group at install time)
- [ ] `EnsureWeaverOwnerStep()` is the second step in `NewSelfInstallWorkflow` (after `CheckPrivilegesStep`, before `SetupHomeDirectoryStructure`)
- [ ] `EnsureWeaverOwnerStep()` is the FIRST step in both branches of `InstallHandler.BuildWorkflow` (cluster-created and cluster-not-created paths) so `block node install` is self-sufficient

### Compat test fix
- [ ] `uat:compat` in `taskfiles/uat.yaml` has a new Step 3 that manually runs `groupadd`/`useradd` to create weaver:2500 before calling `block node install` with the old binary
- [ ] `groupadd`/`useradd` use `|| true` so re-runs don't fail if the accounts already exist
- [ ] Step numbering in compat task: 1=install-old, 2=record-version, 3=create-accounts, 4=block-node-install, 5=verify, 6=record-state, 7=upgrade-CLI, 8=verify-new-version, 9=block-node-upgrade, 10-12=verify

### SystemUser consolidation
- [ ] `ServiceAccount` type no longer exists in `pkg/security/security.go`
- [ ] `HederaOwner` type no longer exists
- [ ] `SystemUser` struct has `UserName`, `UserId`, `GroupName`, `GroupId` fields
- [ ] `SetWeaverUser(u SystemUser)` replaces `SetServiceAccount`
- [ ] `SetHederaUser(u SystemUser)` replaces `SetHederaOwner`
- [ ] `pkg/config/svc_acc.go` and `hedera_owner.go` deleted; `identities.go` is their replacement
- [ ] No call sites use old names (`ServiceAccount*`, `HederaOwner*`) -- verify with grep

### Preflight cleanup
- [ ] `CheckWeaverUserStep` validates only `weaver:2500` -- hedera:2000 block fully removed
- [ ] weaver `useradd` instructions show nologin system account flags (`-r -M -s /usr/sbin/nologin`)

## Test Commands

```bash
# Lint
task lint

# macOS unit tests (security + principal packages)
task test:coverage TEST_PATHS=./pkg/security/... TEST_REGEX='.'

# macOS unit tests (state package -- atomic_write fix)
task test:coverage TEST_PATHS=./internal/state/... TEST_REGEX='.'

# Linux cross-compile check (catches build-tag errors on Linux-only packages)
GOOS=linux GOARCH=amd64 go build ./...

# Full unit test suite in UTM VM (required for Linux-only packages)
task vm:test:unit
```

## Manual UAT (UTM VM)

### 0. Prerequisites -- no manual user creation needed

```bash
# Ensure neither weaver nor hedera exists before testing
sudo groupdel weaver 2>/dev/null; sudo userdel weaver 2>/dev/null
sudo groupdel hedera 2>/dev/null; sudo userdel hedera 2>/dev/null
```

### 1. Run provisioner install -- weaver auto-created

```bash
sudo solo-provisioner install

# Confirm weaver:2500 was created
id weaver
# Expected: uid=2500(weaver) gid=2500(weaver) groups=2500(weaver)

# Confirm nologin (not a login user)
getent passwd weaver
# Expected: weaver:x:2500:2500::/:/usr/sbin/nologin
```

### 2. Verify provisioner home dirs are root:weaver 2775 (setgid)

```bash
stat -c '%a %U:%G %n' /opt/solo/weaver/bin
# Expected: 2775 root:weaver

stat -c '%a %U:%G %n' /opt/solo/weaver/logs
# Expected: 2775 root:weaver

stat -c '%a %U:%G %n' /opt/solo/weaver/state
# Expected: 2775 root:weaver
```

### 3. Verify sandbox dirs are root:root 0755

```bash
stat -c '%a %U:%G %n' /opt/solo/weaver/sandbox/bin
# Expected: 755 root:root
```

### 4. Verify state.yaml and log file are group-readable

```bash
stat -c '%a %U:%G %n' /opt/solo/weaver/state/state.yaml
# Expected: 640 root:weaver

stat -c '%a %U:%G %n' /opt/solo/weaver/logs/solo-provisioner.log
# Expected: 640 root:weaver
```

### 5. Run block node install -- hedera auto-created

```bash
# Confirm hedera does not exist before install
getent group hedera || echo 'hedera group not found'

sudo solo-provisioner block node install ...

# Confirm hedera:2000 was created automatically
id hedera
# Expected: uid=2000(hedera) gid=2000(hedera) groups=2000(hedera)

# Confirm weaver is in the hedera group
id weaver
# Expected: groups include hedera (2000)
```

### 6. Verify block-node storage is hedera:hedera 2775

```bash
stat -c '%a %U:%G %n' /opt/solo/weaver/block-nodes/live
# Expected: 2775 hedera:hedera
```

### 7. Verify GID/UID mismatch detection

```bash
# Pre-create hedera with wrong GID
sudo groupadd -g 9999 hedera && sudo useradd -r -u 9999 -g 9999 -M -s /usr/sbin/nologin hedera
sudo solo-provisioner block node install ...
# Expected: fails with 'hedera owner group has incorrect GID' and groupmod fix instructions

sudo groupdel hedera; sudo userdel hedera
```

### 8. Verify cilium recovers (sandbox dirs no longer break mount-cgroup)

```bash
kubectl get pods -n kube-system -l k8s-app=cilium
# Expected: Running
```

### 9. Verify weaver daemon can read state.yaml and write logs

```bash
sudo systemctl start solo-provisioner
sudo systemctl status solo-provisioner
# Expected: active (running) -- no permission errors

sudo -u weaver cat /opt/solo/weaver/state/state.yaml
# Expected: shows state content (not permission denied)
```

### 10. Verify preflight no longer requires hedera pre-existence

```bash
# On a clean system (after solo-provisioner install, before block node install):
sudo solo-provisioner kube cluster install --profile=local --node-type=block
# Expected: succeeds -- preflight does not check for hedera:2000
```
