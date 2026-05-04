# Security Model: Two-User Identity and File Ownership

## Overview

The `solo-provisioner` uses two distinct Unix identities:

| Identity | User | UID | Purpose |
|----------|------|-----|---------|
| **Process user** | `weaver` | 2500 | Runs the `solo-provisioner` daemon via systemd |
| **Storage owner** | `hedera` | 2000 | Owns block-node storage paths; matches the UID hardcoded in block-node Helm init containers |

## Why Two Users

Running a long-lived daemon as `root` is bad practice — a compromised daemon would have
unrestricted access to the host. The `weaver` user is a minimal-privilege account that
runs the daemon without root.

However, block-node Helm init containers run as UID 2000 (`hedera`) and need **write**
access to the storage directories they initialize. File ownership must therefore match
that UID for those paths.

## How weaver Writes Files That hedera Can Access

A non-root process cannot call `chown` to transfer file ownership to another user — that
requires `root` or `CAP_CHOWN`. Instead of runtime `chown`, the setup uses **setgid
directories** and **group membership**:

1. **`weaver` is added to the `hedera` group** at install time via a pure-Go
   `Manager.AddUserToGroup` call (flock + truncate-rewrite of `/etc/group`).
2. **Block-node storage directories** are created owned by `hedera:hedera` with mode
   `2775` (setgid + group-writable) — done once at install time while running as root.
3. Files written by the daemon **inherit the `hedera` group automatically** via the
   setgid bit — no explicit `chown` call needed at runtime.
4. The block-node container (UID 2000) accesses files via **owner** bits; the daemon
   (weaver, in the hedera group) accesses them via **group** bits.

```
Directory:  hedera:hedera  2775  (setgid)
  \_ file.jar  weaver:hedera  0664   <- hedera group inherited from parent via setgid
```

For paths where containers only need **read** access (rendered configs, Helm values),
standard `0644`/`0755` permissions are sufficient — the container reads via the `other`
bits and no group membership is required.

## Directory Permission Matrix

| Path | Owner | Mode | Rationale |
|------|-------|------|----------|
| Block-node storage dirs (`/opt/solo/weaver/block-nodes/live`) | `hedera:hedera` | `2775` | Container write (owner) + daemon write (group via setgid) |
| Provisioner home dirs (`/opt/solo/weaver/`) — non-sandbox | `root:weaver` | `2775` | Daemon writes here via group membership; setgid propagates weaver group to new files |
| Sandbox dirs (`/opt/solo/weaver/sandbox/`) | `root:root` | `0755` | System containers (e.g. Cilium) bind-mount these and are not in the weaver group; must be world-readable |
| Daemon home (`/home/weaver`) | `weaver:weaver` | `0750` | Private home for daemon; holds `.kube/config` for cluster API access |
| Daemon kubeconfig (`/home/weaver/.kube/`) | `weaver:weaver` | dir default | Written by `configureWeaverKubeConfig` at install time |
| Rendered/temp config files | process-owned | `0644` | Read-only by containers via `other` bits |

## Runtime: No chown Needed

The daemon runs as `weaver:2500`. It writes to setgid-enabled directories via normal
file I/O — `fsx.Manager.WriteFile`, `os.WriteFile`, etc. The kernel assigns the `hedera`
group to new files automatically. No `CAP_CHOWN`, no `sudo chown`, no ownership-setting
code in the hot path.

Ownership and the setgid bit are set **once**, at install time, by the setup workflow
running as root:

```bash
# Done by block node install workflow (SetupStorage)
chown hedera:hedera /opt/solo/weaver/block-nodes/live
chmod 2775          /opt/solo/weaver/block-nodes/live

# Done by EnsureHederaOwnerStep — pure Go, no shell
# Manager.AddUserToGroup rewrites /etc/group under exclusive flock
pm.AddUserToGroup("weaver", "hedera")
```

## Privilege Escalation

The `weaver` user has a passwordless sudoers entry for specific binaries:

```
weaver ALL=(root) NOPASSWD: /usr/bin/helm, /usr/bin/kubectl, /usr/sbin/kubeadm, \\
  /bin/systemctl, /usr/bin/systemctl, /bin/mkdir
```

This covers Kubernetes and Helm operations that require elevated privileges. It does
**not** grant a general root shell — only the listed binaries are accessible.

## Key Code Locations

| File | Role |
|------|------|
| `pkg/config/identities.go` | `Weaver*()` (weaver:2500) and `Hedera*()` (hedera:2000) name/ID accessors; `WeaverHomeDir()` returning `/home/weaver` |
| `pkg/security/security.go` | `SystemUser` struct and ACL constants (`StorageDirPerm = 0o2775`, etc.) |
| `pkg/security/principal/manager_create_unix.go` | `CreateUserWithId(name, uid, homeDir)`, `AddUserToGroup` (validates + idempotency + delegates to Provider) |
| `pkg/security/principal/provider_unix.go` | `WriteUserEntry(name, uid, homeDir)` writes correct `/etc/passwd` entry; `AddMemberToGroup` rewrites `/etc/group` under exclusive flock |
| `pkg/models/paths.go` | `DefaultStorageDirPerm = 0o2775` (setgid + group-writable) |
| `internal/blocknode/storage.go` | Creates block-node dirs with `DefaultStorageDirPerm` and `hedera:hedera` ownership |
| `internal/blocknode/migration_storage.go` | Same pattern for storage migrations |
| `internal/workflows/steps/step_ensure_weaver_owner.go` | Creates weaver:2500 account with home `/home/weaver`; sets `weaver:weaver 0750` on home dir |
| `internal/workflows/steps/step_ensure_hedera_owner.go` | Creates hedera:2000 account; calls `pm.AddUserToGroup("weaver", "hedera")` |
| `internal/workflows/steps/step_setup_directories.go` | Creates provisioner home dirs (`root:weaver 2775`) and sandbox dirs (`root:root 0755`) |
| `internal/kube/admin.go` | `configureWeaverKubeConfig()` copies kubeconfig into `/home/weaver/.kube/` and sets ownership |
| `internal/templates/files/weaver/sudoers` | Sudoers entry granting weaver passwordless access to specific binaries |
| `internal/templates/files/weaver/solo-provisioner.service` | Systemd unit running the daemon as `weaver:weaver` |

## Important Constraints

- **`hedera:2000`** UID is hardcoded in the block-node Helm init containers and **must
  never be changed** via a config file. `pkg/config/identities.go` is intentionally
  not wired to any YAML field.

- **Sandbox directories** must remain `root:root 0755` — system containers such as Cilium
  bind-mount them and are not members of the weaver group. Setting them setgid-weaver
  would break those mounts.

When in doubt about which identity to use:

- Block-node storage directories → `HederaUserName()` / `HederaGroupName()`
- Weaver daemon home and kubeconfig → `WeaverUserName()` / `WeaverGroupName()` / `WeaverHomeDir()`
- Files written at runtime by the daemon → no explicit ownership needed (setgid handles it)
