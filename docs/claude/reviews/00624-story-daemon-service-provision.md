# Review Guide — #624 daemon service provision

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/624
> **PR base:** `00499-feat-solo-provisioner-daemon-core`

## Summary

Extends `solo-provisioner daemon service install` into a full provisioning workflow that
creates K8s RBAC resources and writes the daemon kubeconfig before starting the systemd
service. Adds `daemon service start` and `daemon service stop` commands.

## Changed files

| File | Change |
|---|---|
| `pkg/models/weaver_paths.go` | Add `DaemonKubeconfigPath` (`$home/config/daemon.kubeconfig`) |
| `internal/workflows/steps/step_daemon_provision.go` | New — `CheckClusterStep`, `CreateDaemonRBACStep`, `WriteDaemonKubeconfigStep`, `RemoveDaemonKubeconfigStep`, `DeleteDaemonRBACStep` |
| `internal/workflows/daemon.go` | Extend install/uninstall workflows with RBAC + kubeconfig steps; return `error` from constructors |
| `cmd/cli/commands/daemon/service/install.go` | Handle `error` return from workflow constructor |
| `cmd/cli/commands/daemon/service/uninstall.go` | Handle `error` return from workflow constructor |
| `cmd/cli/commands/daemon/service/start.go` | New — `daemon service start` via `pkgos.RestartService` |
| `cmd/cli/commands/daemon/service/stop.go` | New — `daemon service stop` via `pkgos.StopService` |
| `cmd/cli/commands/daemon/service/service.go` | Register `start` and `stop` commands; add `daemonServiceName` constant |

## Review checklist

- [ ] `CreateDaemonRBACStep` is idempotent — existing SA/ClusterRole/CRB/Secret are left unchanged (`AlreadyExists` errors ignored)
- [ ] `WriteDaemonKubeconfigStep` writes kubeconfig with mode 0600 (root-only)
- [ ] `waitForSAToken` polls until the K8s token controller populates the secret, with a 30s deadline
- [ ] Kubeconfig uses a long-lived `kubernetes.io/service-account-token` Secret (not a `TokenRequest`) — token does not expire at runtime
- [ ] `deleteDaemonRBAC` is best-effort: `IsNotFound` errors are silently ignored; other errors are logged as warnings
- [ ] Install rollback chain: `WriteDaemonKubeconfigStep.Rollback` removes kubeconfig file; `CreateDaemonRBACStep.Rollback` deletes all four RBAC resources
- [ ] Uninstall order: Stop service → remove unit file → remove kubeconfig → delete RBAC
- [ ] `loadOrbit` reads `daemon.yaml` (via `LoadDaemonConfig`) before the workflow runs — fails fast if config is missing or `orbit` is empty
- [ ] `daemon service start` uses `RestartService` (idempotent: starts if stopped, restarts if already running)
- [ ] `daemon service stop` uses `StopService` (best-effort: exits cleanly even if service is not running)
- [ ] `daemonServiceName` constant defined in `service.go` — used by all four service sub-commands

## Test commands

```bash
# Unit tests (macOS safe)
task test:unit

# Lint
task lint
```

## Manual UAT (UTM VM with K8s cluster)

```bash
# Precondition: daemon.yaml exists with valid orbit
cat /opt/solo/weaver/config/daemon.yaml

# Install (provisions RBAC, kubeconfig, service)
sudo solo-provisioner daemon service install

# Verify K8s resources
kubectl get sa solo-provisioner-daemon -n <orbit>
kubectl get clusterrole solo-provisioner-daemon
kubectl get clusterrolebinding solo-provisioner-daemon
kubectl get secret solo-provisioner-daemon-token -n <orbit>

# Verify kubeconfig and service
ls -la /opt/solo/weaver/config/daemon.kubeconfig  # should be 0600
systemctl status solo-provisioner-daemon            # active (running)

# Stop and start
sudo solo-provisioner daemon service stop
systemctl status solo-provisioner-daemon            # inactive
sudo solo-provisioner daemon service start
systemctl status solo-provisioner-daemon            # active (running)

# Uninstall (removes service, kubeconfig, RBAC)
sudo solo-provisioner daemon service uninstall
kubectl get sa solo-provisioner-daemon -n <orbit>   # should not exist
ls /opt/solo/weaver/config/daemon.kubeconfig        # should not exist
```
