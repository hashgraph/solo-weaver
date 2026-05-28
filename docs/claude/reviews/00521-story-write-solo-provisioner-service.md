# Review Guide — #521 solo-provisioner-daemon.service + CLI install/uninstall

## Problem & Solution

The existing `solo-provisioner.service` template was wrong: `Type=simple`, wrong binary
(`solo-provisioner daemon`), and its install/remove steps were commented out with no CLI
entry point. This PR replaces it with a correct `solo-provisioner-daemon.service`
(`Type=notify`, `Restart=always`, proper sandbox) and adds
`solo-provisioner daemon service install/uninstall` CLI commands to deploy it.

## Changed Files

| File | Description |
|---|---|
| `internal/templates/files/weaver/solo-provisioner.service` | **Deleted** — was wrong type/binary, not in use |
| `internal/templates/files/weaver/solo-provisioner-daemon.service` | **New** — correct unit file (`Type=notify`, `Restart=always`, sandbox hardening) |
| `internal/templates/files/weaver/sudoers` | Added `solo-provisioner` binary paths so `weaver` user can call `sudo solo-provisioner self-upgrade` |
| `internal/workflows/steps/step_weaver.go` | Removed dead `InstallWeaverServiceStep`, `RemoveWeaverServiceStep`, and their constants |
| `internal/workflows/steps/step_daemon.go` | **New** — `InstallDaemonServiceStep` / `RemoveDaemonServiceStep` workflow steps |
| `internal/workflows/weaver.go` | Wired `InstallSudoersStep` / `RemoveSudoersStep` (were commented out); removed dead service step refs |
| `internal/workflows/daemon.go` | **New** — `NewDaemonServiceInstallWorkflow` / `NewDaemonServiceUninstallWorkflow` |
| `cmd/cli/commands/daemon/daemon.go` | **New** — `daemon` parent Cobra command |
| `cmd/cli/commands/daemon/service/service.go` | **New** — `daemon service` subcommand |
| `cmd/cli/commands/daemon/service/install.go` | **New** — `daemon service install` leaf command |
| `cmd/cli/commands/daemon/service/uninstall.go` | **New** — `daemon service uninstall` leaf command |
| `cmd/cli/commands/root.go` | Wired `daemon.GetCmd()` into root |

## Code Review Checklist

- [ ] Service file has `Type=notify` (not `Type=simple`) — required for sd_notify integration (#527)
- [ ] `ExecStart` points to `/opt/solo/weaver/bin/solo-provisioner-daemon` (daemon binary, not CLI)
- [ ] `ReadWritePaths=/opt/solo /opt/hgcapp` covers all paths the daemon writes to
- [ ] `ProtectSystem=strict` does not block the daemon's read-only access to system paths
- [ ] `NoNewPrivileges=true` does not break `sudo solo-provisioner self-upgrade` — daemon calls `sudo`, not setuid
- [ ] sudoers template includes both `/opt/solo/weaver/bin/solo-provisioner` and `/usr/local/bin/solo-provisioner`
- [ ] `InstallDaemonServiceStep` rollback disables the service and removes the unit file
- [ ] Dead `InstallWeaverServiceStep` / `RemoveWeaverServiceStep` fully removed (no lingering references)
- [ ] `InstallSudoersStep` / `RemoveSudoersStep` now active in self-install/uninstall workflows
- [ ] `daemon service install` wraps with `CheckPrivilegesStep()` (requires root)
- [ ] CLI command tree follows the same pattern as `teleport` (parent → subcommand → leaf)
- [ ] `daemon.GetCmd()` registered in `root.go` before `version.Cmd()` (alphabetical order)

## Test Commands

```bash
# Unit tests (macOS-safe)
task test:unit

# Targeted coverage for new packages
task test:coverage TEST_PATHS=./cmd/cli/commands/daemon/... TEST_REGEX="."
task test:coverage TEST_PATHS=./internal/workflows/... TEST_REGEX="."

# Full unit suite in UTM VM
task vm:test:unit
```

## Manual UAT

### Prerequisites
- UTM VM running (`task vm:start`)
- Build: `task build:cli GOOS=linux GOARCH=amd64`
- Copy binary to VM and run `sudo solo-provisioner install`

### 1. Install daemon service

```bash
sudo solo-provisioner daemon service install
```

Expected output:
```
✓ Checking privileges
✓ Installing solo-provisioner-daemon systemd service
solo-provisioner-daemon service installed and enabled; start it with: systemctl start solo-provisioner-daemon
```

Verify:
```bash
systemctl status solo-provisioner-daemon
# Expected: loaded, enabled (inactive/dead — binary not yet deployed)

cat /etc/systemd/system/solo-provisioner-daemon.service
# Expected: Type=notify, ExecStart=/opt/solo/weaver/bin/solo-provisioner-daemon
```

### 2. Uninstall daemon service

```bash
sudo solo-provisioner daemon service uninstall
```

Expected output:
```
✓ Checking privileges
✓ Removing solo-provisioner-daemon systemd service
```

Verify:
```bash
ls /etc/systemd/system/solo-provisioner-daemon.service
# Expected: No such file or directory
```

### 3. Verify sudoers entry

```bash
sudo solo-provisioner install
cat /etc/sudoers.d/solo-provisioner
# Expected: includes /opt/solo/weaver/bin/solo-provisioner and /usr/local/bin/solo-provisioner

# Verify weaver user can invoke the binary via sudo
sudo -u weaver sudo solo-provisioner version
# Expected: version output, no permission error
```

### 4. Verify legacy service file is gone

```bash
ls /etc/systemd/system/solo-provisioner.service 2>&1
# Expected: No such file or directory (was never installed)
```
