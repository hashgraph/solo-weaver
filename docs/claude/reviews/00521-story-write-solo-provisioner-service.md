# Review Guide — #521/#522 solo-provisioner-daemon.service + CLI service commands

> **Issues:** https://github.com/hashgraph/solo-weaver/issues/521,
> https://github.com/hashgraph/solo-weaver/issues/522
> **PR:** #623 — base `00499-feat-solo-provisioner-daemon-core`

## Summary

Adds the `solo-provisioner-daemon.service` systemd unit file (sandbox+symlink pattern)
and the `solo-provisioner daemon service install|check|uninstall` CLI commands.
Activates `InstallSudoersStep`/`RemoveSudoersStep` in the self-install workflows and
restricts the sudoers NOPASSWD entry to `solo-provisioner self-upgrade [args]` only.

## Changed files

| File | Change |
|---|---|
| `internal/templates/files/weaver/solo-provisioner-daemon.service` | New — `Type=notify`, sandbox hardening, no `NoNewPrivileges` |
| `internal/templates/files/weaver/solo-provisioner.service` | Deleted — was wrong type/binary, not in use |
| `internal/templates/files/weaver/sudoers` | Add solo-provisioner `self-upgrade` NOPASSWD (no-arg + wildcard forms); remove broad binary grant |
| `internal/workflows/steps/step_daemon.go` | New — `InstallDaemonServiceStep`, `RemoveDaemonServiceStep`, `CheckDaemonServiceStep` using sandbox+symlink pattern; binary path derived from `paths.BinDir` |
| `internal/workflows/steps/step_weaver.go` | Remove dead service steps; keep sudoers steps |
| `internal/workflows/steps/step_daemon_it_test.go` | New — 4 integration tests (root-skipped when euid != 0) |
| `internal/workflows/daemon.go` | New — `NewDaemonServiceInstallWorkflow`, `NewDaemonServiceUninstallWorkflow`, `NewDaemonServiceCheckWorkflow` |
| `internal/workflows/weaver.go` | Uncomment `InstallSudoersStep`/`RemoveSudoersStep` |
| `pkg/models/weaver_paths.go` | Add `DaemonServiceSandboxPath`, `DaemonServiceSymlinkPath`; add sandbox systemd dir |
| `cmd/cli/commands/daemon/` | New — `daemon`, `daemon service`, `install`, `uninstall`, `check` Cobra commands |
| `cmd/cli/commands/root.go` | Register `daemon` command |

## Review checklist

- [ ] Unit file uses `Type=notify` — `systemctl start` will block until `READY=1` (implemented in #527)
- [ ] `NoNewPrivileges` is intentionally absent — daemon needs setuid for `sudo solo-provisioner self-upgrade`
- [ ] `ProtectSystem=strict` + `ReadWritePaths=/opt/solo /opt/hgcapp` provides sandbox hardening without breaking write paths
- [ ] Unit file written to `$home/sandbox/usr/lib/systemd/system/`; symlinked to `/usr/lib/systemd/system/` (matches kubelet/crio pattern)
- [ ] `installDaemonServiceFiles` cleans up sandbox file if symlink creation fails (no half-installed state)
- [ ] `RemoveDaemonServiceStep` calls `StopService` before `DisableService` (disable does not stop a running unit)
- [ ] `CheckDaemonServiceStep` verifies sandbox file, symlink target, enabled, running, binary (via `paths.BinDir`), sudoers, Unix socket health
- [ ] Unix socket health check transport has `Proxy: nil` — prevents HTTP_PROXY env vars intercepting local socket requests
- [ ] Sudoers restricts to `solo-provisioner self-upgrade` only (no-arg + wildcard both listed; `*` alone requires at least one argument)
- [ ] Integration tests skip when `euid != 0`; tests call unexported helpers directly (same package, no export shims)

## Test commands

```bash
# Unit tests (macOS safe)
task test:unit

# Integration tests (UTM VM, root required)
task vm:test:integration TEST_NAME='^Test_DaemonService'
task vm:test:integration TEST_NAME='^Test_InstallDaemonServiceStep'
task vm:test:integration TEST_NAME='^Test_RemoveDaemonServiceStep'
```

## Manual UAT (UTM VM)

```bash
# Install
sudo solo-provisioner daemon service install
systemctl status solo-provisioner-daemon   # active (activating) — waits for READY=1

# Check
sudo solo-provisioner daemon service check

# Uninstall
sudo solo-provisioner daemon service uninstall
ls /usr/lib/systemd/system/solo-provisioner-daemon.service  # should not exist
```

## Sudoers verification

```bash
# As weaver user — these should work
sudo /opt/solo/weaver/bin/solo-provisioner self-upgrade
sudo /opt/solo/weaver/bin/solo-provisioner self-upgrade --force

# These should be denied
sudo /opt/solo/weaver/bin/solo-provisioner install
sudo /opt/solo/weaver/bin/solo-provisioner kube cluster install
```
