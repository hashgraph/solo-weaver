# #521 — Write solo-provisioner-daemon.service systemd unit file

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/521
> **Epic:** #499 — solo-provisioner-daemon Core
> **Story branch:** `00521-story-write-solo-provisioner-service`
> **PR base:** `00499-feat-solo-provisioner-daemon-core` (branched from `origin/00499...` @ `daf3430`)
> **PR closes:** #521

## Summary

Replace the placeholder `solo-provisioner.service` (wrong binary/type) with a proper
`solo-provisioner-daemon.service` unit file (`Type=notify`, `Restart=always`,
`ExecStart=/opt/solo/weaver/bin/solo-provisioner-daemon`) and implement two CLI
commands — `solo-provisioner daemon service install` and
`solo-provisioner daemon service uninstall` — that copy/enable or disable/remove
the unit using the existing workflow-step pattern.

## Problem

`internal/templates/files/weaver/solo-provisioner.service` is the wrong unit:
- `Type=simple`, `ExecStart=solo-provisioner daemon` — incorrect binary path and type.
- Both `InstallWeaverServiceStep()` and `RemoveWeaverServiceStep()` are **commented out**
  in `internal/workflows/weaver.go` and are not reachable from any CLI command.
- No `daemon` subcommand tree exists under `cmd/cli/commands/`.

The user confirmed `solo-provisioner.service` is **not currently in use**, so no
migration path is needed — just replace it.

## Decisions

| Question | Decision |
|---|---|
| Service file name | `solo-provisioner-daemon.service` (rename from `solo-provisioner.service`) |
| Legacy file | Delete `solo-provisioner.service`; no migration needed (not in use) |
| Service type | `Type=notify` (pairs with sd_notify in story #527) |
| Restart policy | `Restart=always` |
| ExecStart | `/opt/solo/weaver/bin/solo-provisioner-daemon` |
| Sandbox hardening | `NoNewPrivileges=true`, `PrivateTmp=true`, `ProtectSystem=strict`, `ReadWritePaths=/opt/solo /opt/hgcapp` |
| systemd service name | `solo-provisioner-daemon` |
| Step reuse | Rename/update constants in `step_weaver.go`; new `InstallDaemonServiceStep`/`RemoveDaemonServiceStep` (or reuse existing with updated constants) |
| Workflow functions | New `NewDaemonServiceInstallWorkflow()` and `NewDaemonServiceUninstallWorkflow()` in `internal/workflows/weaver.go` |
| CLI placement | `cmd/cli/commands/provisioner/daemon/service/` — `daemon` under new `provisioner` parent OR directly `cmd/cli/commands/daemon/service/`; follow teleport pattern |
| CLI root wiring | Add `daemonCmd` to `root.go` via `rootCmd.AddCommand(daemon.GetCmd())` |
| Privilege check | Wrap each command with `CheckPrivilegesStep()` (service install requires root) |

## Scope

### `internal/templates/files/weaver/`
- [ ] Delete `solo-provisioner.service`
- [ ] Add `solo-provisioner-daemon.service` with `Type=notify`, `Restart=always`, sandbox hardening

### `internal/workflows/steps/step_weaver.go`
- [ ] Update constants: `serviceTemplatePath`, `serviceDstPath`, `weaverServiceName` → point to daemon service
- [ ] Rename `InstallWeaverServiceStep` → `InstallDaemonServiceStep`, `RemoveWeaverServiceStep` → `RemoveDaemonServiceStep`

### `internal/workflows/weaver.go`
- [ ] Add `NewDaemonServiceInstallWorkflow()` using `InstallDaemonServiceStep()`
- [ ] Add `NewDaemonServiceUninstallWorkflow()` using `RemoveDaemonServiceStep()`
- [ ] Uncomment / wire the steps (they were commented out previously)

### `cmd/cli/commands/daemon/` (new package tree)
- [ ] `daemon.go` — `daemonCmd` parent (`Use: "daemon"`, `common.DefaultRunE`), exposes `GetCmd()`
- [ ] `service/service.go` — `serviceCmd` parent (`Use: "service"`, `common.DefaultRunE`), exposes `GetCmd()`
- [ ] `service/install.go` — `installCmd` that calls `RunWorkflowBuilder(ctx, NewDaemonServiceInstallWorkflow())`
- [ ] `service/uninstall.go` — `uninstallCmd` that calls `RunWorkflowBuilder(ctx, NewDaemonServiceUninstallWorkflow())`

### `cmd/cli/commands/root.go`
- [ ] Import and wire `rootCmd.AddCommand(daemon.GetCmd())`

## Out of scope

- `sd_notify` READY/STOPPING calls inside the daemon binary (story #527)
- `WantedBy=solo-provisioner.target` or dependency ordering beyond `After=network.target`
- Taskfile build target for `solo-provisioner-daemon` (story #516)

## Test plan

- [ ] Unit: `task test:unit` (macOS-safe; no Linux-only deps in this change)
- [ ] Targeted: `task test:coverage TEST_PATHS=./cmd/cli/commands/daemon/... TEST_REGEX="."`
- [ ] Lint: `task lint` after all changes
- [ ] Manual UAT (UTM VM):
  1. Build: `task build:cli GOOS=linux GOARCH=amd64`
  2. Run: `sudo solo-provisioner daemon service install`
  3. Verify: `systemctl status solo-provisioner-daemon` → `loaded` (not yet started — no binary yet)
  4. Run: `sudo solo-provisioner daemon service uninstall`
  5. Verify: unit file removed from `/etc/systemd/system/`

## Risks / rollbacks

- `solo-provisioner.service` is confirmed not in use; deleting it is safe.
- Renaming steps breaks the commented-out call sites in `weaver.go` — those are updated in the same PR.
- `InstallDaemonServiceStep` requires root; `CheckPrivilegesStep()` enforces this.
