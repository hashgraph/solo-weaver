# #624 — daemon service provision: RBAC, kubeconfig, start/stop commands

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/624
> **Epic:** #499 — Epic A38_2 solo-provisioner-daemon Core
> **Story branch:** `00624-story-daemon-service-provision`
> **PR base:** `00499-feat-solo-provisioner-daemon-core` (branched @ `d75aaf3`)
> **PR closes:** #624

## Summary

Extends `solo-provisioner daemon service install` into a full provisioning workflow
and adds `daemon service start` / `daemon service stop` commands. After this PR, a
single `sudo solo-provisioner daemon service install` creates all K8s RBAC resources,
writes the daemon kubeconfig, installs and starts the systemd service — making the
daemon fully operational end-to-end.

## Problem

The existing install workflow (`internal/workflows/daemon.go:NewDaemonServiceInstallWorkflow`)
only installs the systemd unit file and starts the service. It does not:
- Check the K8s cluster is reachable
- Create the `solo-provisioner-daemon` ServiceAccount, ClusterRole, ClusterRoleBinding
- Generate the daemon kubeconfig at `/opt/solo/weaver/config/daemon.kubeconfig`

Without these, `daemon.Run()` fails the kubeconfig preflight check immediately and
the service never reaches `active (running)`.

## Decisions

| Question | Decision |
|---|---|
| Admin kubeconfig | `~/.kube/config` — already used by the weaver user for all cluster ops |
| Daemon kubeconfig path | `/opt/solo/weaver/config/daemon.kubeconfig` — add `DaemonKubeconfigPath` to `WeaverPaths` |
| SA / ClusterRole name | `solo-provisioner-daemon` — cluster-scoped, matches the service name |
| RBAC verbs | `list`, `watch` on `networkupgradeexecutes.hedera.com` — exactly what `probeKubeRBAC` checks |
| Kubeconfig generation | Extract SA token secret → write minimal kubeconfig YAML using the SA token and cluster CA |
| K8s client for provisioning | `internal/kube` package (`NewClient()` reads `~/.kube/config`) |
| Step implementation | New steps in `internal/workflows/steps/step_daemon_provision.go` |
| Workflow wiring | Replace `NewDaemonServiceInstallWorkflow` body; add RBAC + kubeconfig steps before existing unit-file step |
| Uninstall order | Stop → disable/remove unit → delete kubeconfig → delete CRB → delete CR → delete SA |
| `start` / `stop` commands | Thin Cobra commands calling `pkgos.StartService` / `pkgos.StopService` directly (no workflow needed) |
| Orbit namespace | Read from `daemon.yaml` (`cfg.Orbit`) — already loaded by `models.Paths()` |
| Error library | `errorx` throughout — no `fmt.Errorf` or `errors.New` |

## Scope

### `pkg/models/weaver_paths.go`
- [ ] Add `DaemonKubeconfigPath string` (`$home/config/daemon.kubeconfig`)
- [ ] Populate in `NewWeaverPaths`

### `internal/workflows/steps/step_daemon_provision.go` (new)
- [ ] `CheckClusterStep()` — verify K8s API reachable via `~/.kube/config`
- [ ] `CreateDaemonRBACStep(namespace string)` — idempotent create of SA + ClusterRole + ClusterRoleBinding; rollback deletes all three
- [ ] `WriteDaemonKubeconfigStep(paths WeaverPaths, namespace string)` — extract SA token, write kubeconfig YAML; rollback removes file
- [ ] `RemoveDaemonKubeconfigStep(paths WeaverPaths)` — remove kubeconfig file (best-effort, no error on missing)
- [ ] `DeleteDaemonRBACStep(namespace string)` — delete CRB + CR + SA (best-effort)

### `internal/workflows/daemon.go`
- [ ] `NewDaemonServiceInstallWorkflow()` — prepend `CheckClusterStep`, `CreateDaemonRBACStep`, `WriteDaemonKubeconfigStep` before existing `InstallDaemonServiceStep`
- [ ] `NewDaemonServiceUninstallWorkflow()` — append `RemoveDaemonKubeconfigStep`, `DeleteDaemonRBACStep` after existing `RemoveDaemonServiceStep`

### `cmd/cli/commands/daemon/service/` (extend existing)
- [ ] `start.go` — `solo-provisioner daemon service start` → `pkgos.StartService(ctx, daemonServiceName)`
- [ ] `stop.go` — `solo-provisioner daemon service stop` → `pkgos.StopService(ctx, daemonServiceName)`
- [ ] Register both in `service.go`

## Out of scope

- `daemon service check` extension (covered by existing `CheckDaemonServiceStep`)
- Kubeconfig rotation / renewal
- Multi-cluster support
- Integration tests requiring a live cluster (tagged `require_cluster`)

## Test plan

- [ ] Unit: `task test:unit` — new step constructors compile and wire correctly
- [ ] Lint: `task lint`
- [ ] Manual UAT (UTM VM with K8s cluster):
  1. `sudo solo-provisioner daemon service install`
  2. `kubectl get sa solo-provisioner-daemon -n <orbit>` — exists
  3. `kubectl get clusterrolebinding solo-provisioner-daemon` — exists
  4. `cat /opt/solo/weaver/config/daemon.kubeconfig` — valid kubeconfig
  5. `systemctl status solo-provisioner-daemon` → `active (running)`
  6. `sudo solo-provisioner daemon service stop` → service stopped
  7. `sudo solo-provisioner daemon service start` → service running again
  8. `sudo solo-provisioner daemon service uninstall` — all resources removed

## Risks / rollbacks

- SA token extraction varies between K8s versions (< 1.24 auto-creates secret; >= 1.24 needs
  `TokenRequest` API). Will use `TokenRequest` (v1, supported since 1.22) for portability.
- RBAC step rollback on partial failure: if CRB creation succeeds but CR deletion fails during
  rollback, orphaned resources remain — acceptable for now; uninstall workflow cleans them up.
- `daemon.yaml` must exist (with valid `orbit`) before install — already enforced by `New()`.
- Kubeconfig file written to `ConfigDir` which requires the directory to exist — `SetupDirectoriesStep`
  runs at self-install time and creates it.
- `start` / `stop` commands require root (systemctl) — consistent with install/uninstall.
- Kubeconfig preflight in `daemon.Run()` validates the file immediately on service start — any
  kubeconfig write error will surface as a service start failure.
- **Rollback note on `TokenRequest`**: token has a default expiry (1h by default from the API
  server). For a long-lived daemon kubeconfig we must request a long expiry or use a
  `kubernetes.io/service-account-token` secret. Decision: create a token Secret manually
  (consistent with other kubeconfig generation in the codebase) and use its token.
  This avoids time-bounded tokens that expire while the daemon is running.
  This is a risk item to discuss before implementation begins.
- **Dependency on daemon.yaml**: `orbit` field must be set before install. The install workflow
  will fail fast if `daemon.yaml` is missing or `orbit` is empty.
- **Existing worktrees**: other open PRs on the epic branch will need to rebase after this
  merges since it modifies `NewDaemonServiceInstallWorkflow`.
- **CLI note on `start`/`stop`**: These are privileged operations but have no rollback step
  — if start fails the operator should check `journalctl -u solo-provisioner-daemon` for details.
  If stop fails (service not running) we treat it as a no-op success.
- **Kubeconfig permissions**: file is written with 0600 (only root-readable) since it contains
  a service account token.
