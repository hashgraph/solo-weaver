# solo-provisioner-daemon — CN-upgrade end-to-end test

This is a task-driven runbook for validating the **daemon's HIP-1496 execute
phase** of a *config-only* consensus-node (CN) software upgrade, driven for real
by solo-operator's `beacon`.

Unlike the synthetic monitor tests (hand-applied `NetworkUpgradeExecute` CRs), this
harness deploys a **real v0.74.0 single-node consensus network** on a single-node
Kubernetes cluster, then uses `beacon` (the HAPI control plane) to upload the
v0.74.2 package and freeze the network. The consensus node writes the upgrade
marker, NMT extracts special file `0.0.150` into the shared upgrade directory, the
UC creates the `NetworkUpgradeExecute` CR, and the **host daemon** performs the
execute phase. We then verify the daemon's handshake.

The substrate is the daemon's **real install path** — `solo-provisioner kube
cluster install` + `kube operator install` (real CRDs) — **not** kind. Every step
is wrapped as a task in [`Taskfile.yml`](Taskfile.yml); run `task -d docs/daemon`
to print the ordered runbook. The VM build/bootstrap/secrets/registry-login tasks
are reused from `taskfiles/uat.yaml` (`uat:*`), not duplicated.

## What this validates

When the freeze fires, the daemon:

1. Triggers on `NetworkUpgradeExecute` reaching `status.phase = ReadyForProvisionerDaemon`.
2. Reads the extracted package from its `upgrade_dir`
   (`/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current`).
3. Creates the **per-operation config CRs** (`<operationId>-<suffix>`, labelled
   `operator.solo.hiero.org/operation-id=<opId>`) — `log4j2configs`,
   `nodesettings`, `applicationproperties`, `throttlesconfigs`, `blocknodesconfigs`, …
   — and waits each to `Valid=True`.
4. Writes the handshake conditions `ConfigCRsApplied=True` and `DaemonResult=True`,
   advances `status.phase` to `PendingNodeUpgrade`, and places
   `infrastructure-versions.yaml` (a no-op when the config-only package ships none).

### Exercised vs. not exercised

**Exercised:** the daemon trigger on `ReadyForProvisionerDaemon`; per-operation
config-CR creation + `Valid=True` wait; the `ConfigCRsApplied` / `DaemonResult`
handshake and phase advance; `infrastructure-versions.yaml` placement; HIP-1496
fatal-vs-transient semantics and the handoff deadline.

**Not exercised (stubs / out of scope):** external-files download (`ExternalFiles`
CR + Job, solo-operator#1325); the runtime safety gate (no-op without an
`infrastructure-versions.yaml`); infra upgrade / cluster teardown-reinstall (blocked
on daemon self-upgrade, #500). Keep the upgrade package **config-only** for a clean run.

## Prerequisites

- **Operator v0.7.0+** (the solo-operator version weaver depends on). The operator
  must let the host daemon own the `PendingInfraUpgrade` / `PendingNodeUpgrade` phase
  transitions and drive the terminal off `DaemonResult`. The `operator` task installs
  it via `kube operator install`.
- **Daemon-delegated execute mode.** The in-pod UC proxy must NOT run the execute
  phase — the `NetworkUpgradeExecute` CR must halt at `ReadyForProvisionerDaemon` and
  wait for the host daemon. This is the **Orbit** setting
  `spec.consensus.provisionerDaemonEnabled: true` (default `false` = cluster-only, UC
  proxy runs execute; `true` = UC runs `UC_MODE=mainnet` and defers to the host
  daemon). The `network` task passes **`consensus node install --provisioner-daemon`**,
  which creates the Orbit in mainnet mode, so the UC comes up in `UC_MODE=mainnet` on
  first reconcile. To flip an *existing* cluster-only Orbit, patch it and recreate the
  CN pod:

  ```bash
  kubectl patch orbit "$NS" --type=merge \
    -p '{"spec":{"consensus":{"provisionerDaemonEnabled":true}}}'
  kubectl -n "$NS" delete pod -l app.kubernetes.io/component=consensus-node
  ```
- **Shared upgrade directory.** The daemon reads the extracted package from its
  `upgrade_dir`; the CN pod extracts `0.0.150` into that same path. On a single host
  both live on the same machine (automatic) — this harness assumes a single-node VM.
- **Daemon-cn RBAC.** Provisioned by `daemon service install` (ServiceAccount,
  ClusterRole, ClusterRoleBinding, token, scoped kubeconfig) — the `daemon:install`
  task, which sanity-checks the grant with `kubectl auth can-i`.
- **Per-node secrets.** `consensus node install` needs the gossip signing secret
  (`node<N>-gossip-keys`) and gRPC TLS secret (`node<N>-grpc-tls-keys`) in the orbit
  namespace, plus a pull secret for private images. The `secrets` task creates these
  from the sample manifests (reuses `uat:secrets`).
- **solo-operator checkout at v0.7.0+** (`SOLO_OPERATOR_DIR`) for `bin/beacon` and
  the deployment zips (`test/dev/staging/build-v0.74.0.zip` + `build-v0.74.2.zip`).
  These are NOT solo-provisioner artifacts — build them with `test/dev`
  (`task setup && task build`); the `zip` task does this.
- **Base deployment package unzipped into `test/data/`.** `consensus node install`
  reads the network's image tags / ledger / chain id from an *unzipped* package
  directory. The `package` task unzips the base zip into the weaver checkout (so the
  VM sees it under `/mnt/solo-weaver`). Manually, that is:

  ```bash
  mkdir -p test/data/build-v0.74.0
  unzip -o "$SOLO_OPERATOR_DIR/test/dev/staging/build-v0.74.0.zip" -d test/data/build-v0.74.0
  ```

Version map (matches solo-operator docs/beacon): deployed **0.74.0**, upgrade to **0.74.2**.

## Quick path

Run everything **inside a Linux VM** (see the repo `CLAUDE.md` VM notes). From the
repo root, `task -d docs/daemon <name>`. Two tasks are long-running and each want
their **own terminal**: `daemon:logs` and `port-forward`.

```
# deployment packages (host — needs a solo-operator checkout)
task -d docs/daemon zip             # build staging/build-v0.74.0 + v0.74.2 zips, print H
task -d docs/daemon package         # unzip the v0.74.0 base package into test/data (--deployment-package-dir)

# substrate — the daemon's real install path (single-node k8s)
task -d docs/daemon rebuild         # build CLI + daemon on the VM + self-install (uat:rebuild)
task -d docs/daemon cluster         # ghcr login + kube cluster install + orbit ns + hedera user + upgrade dir
task -d docs/daemon secrets         # gossip/gRPC + pull secrets (uat:secrets)
task -d docs/daemon operator        # kube operator install (v0.7.0 operator + CRDs)

# a REAL v0.74.0 consensus network, in DAEMON-DELEGATED mode
task -d docs/daemon network         # consensus node install + Orbit provisionerDaemonEnabled=true + genesis

# install + run the host daemon (the execute-phase performer)
task -d docs/daemon daemon:install  # daemon service install (daemon-cn RBAC + scoped kubeconfig)
task -d docs/daemon daemon:logs     # journalctl -f — LEAVE RUNNING in its own terminal

# drive the upgrade with beacon
task -d docs/daemon beacon:build    # build bin/beacon in the solo-operator checkout
task -d docs/daemon config          # write beacon config.yaml + 0.0.2 payer key
task -d docs/daemon port-forward    # node0 HAPI :50211 — LEAVE RUNNING in its own terminal
task -d docs/daemon upload          # upload v0.74.2 into special file 0.0.150 (verify with H)
task -d docs/daemon prepare         # freeze prepare (pinned to H)
task -d docs/daemon freeze          # freeze upgrade (~3 min out) — network freezes, daemon executes

# observe + verify the daemon execute handshake
task -d docs/daemon watch           # watch NetworkUpgradeExecute advance
task -d docs/daemon verify          # assert the handshake + config CRs Valid
task -d docs/daemon version         # beacon network version -> 0.74.2

# clean up
task -d docs/daemon reset           # daemon + cluster uninstall, restore upgrade dir
task -d docs/daemon teardown        # + uat:teardown
```

Override the solo-operator location with `SOLO_OPERATOR_DIR=/path`, the orbit with
`NS=...` (default `solo-orbit`), or the node with `NODE_ID=...` (default `0`).

## Manual step reference

Each task wraps a real command; the notable ones:

| Task | What it runs (essence) |
|---|---|
| `rebuild` | `uat:rebuild` — `task build:cli` (builds daemon too, stamps its digest) + self-install |
| `cluster` | `uat:registry:login:ghcr` + `kube cluster install --non-interactive` + create orbit ns + `hedera` user/group + upgrade staging dir (owned `hedera:hedera`, mode `0775`, `weaver` in the `hedera` group) |
| `secrets` | `uat:secrets NS=<orbit> NODE=node<id>` — pull secret in operator + orbit ns, plus `node<id>-gossip-keys` / `node<id>-grpc-tls-keys` |
| `operator` | `kube operator install --non-interactive`; checks the `networkupgradeexecutes` CRD is registered |
| `package` | host: `unzip` the v0.74.0 base zip into `test/data/build-v0.74.0` (visible in the VM at `/mnt/solo-weaver`) |
| `network` | `consensus node install --experimental --provisioner-daemon --deployment-package-dir <pkg> …` (creates the Orbit in mainnet/daemon-delegated mode), then `consensus network genesis --experimental` |
| `daemon:install` | `daemon service install --components consensus-node --cn-node-id <id> --cn-orbit <ns> --daemon-bin <built>` (`--cn-namespace` is a hidden alias for `--cn-orbit`); then `kubectl auth can-i` checks |
| `zip` | `cd $SOLO_OPERATOR_DIR/test/dev && task setup && task build`; prints `H` (SHA-384 of the upgrade zip) |
| `config` | writes `/tmp/beacon-solo/config.yaml` + the 0.0.2 payer PEM |
| `upload`/`prepare`/`freeze` | `beacon file upload/info`, `beacon freeze prepare/upgrade` — all pinned to `H` |

The daemon binary consumed by `daemon:install` is the dev build at
`/mnt/solo-weaver/bin/solo-provisioner-daemon-linux-<arch>` (passed with
`--daemon-bin`); auto-download of the placeholder `0.0.0` version has no GitHub
release and would fail with resolution hints.

## Verify

After the freeze fires, `task -d docs/daemon verify` runs these checks (namespace = `$NS`):

```bash
# phase advanced to PendingNodeUpgrade
kubectl -n "$NS" get networkupgradeexecute -o jsonpath='{.items[0].status.phase}'

# handshake conditions — expect ConfigCRsApplied=True/Succeeded and DaemonResult=True/Succeeded
kubectl -n "$NS" get networkupgradeexecute -o \
  jsonpath='{range .items[0].status.conditions[*]}{.type}={.status}/{.reason}{"\n"}{end}'

# per-operation config CRs — each reaches Valid=True
OP=$(kubectl -n "$NS" get networkupgradeexecute -o jsonpath='{.items[0].spec.operationId}')
kubectl -n "$NS" get \
  log4j2configs,nodesettings,applicationproperties,throttlesconfigs,blocknodesconfigs \
  -l operator.solo.hiero.org/operation-id=$OP

# daemon execute event log: ExecuteWorkflowStarted … ExecuteWorkflowCompleted
sudo ls /opt/solo/weaver/*/consensus-$OP.jsonl
# infrastructure-versions.yaml is placed only if the package ships one (absent for config-only)
sudo ls -l /opt/solo/weaver/config/infrastructure-versions.yaml

# the network is live on the new build
beacon -c /tmp/beacon-solo/config.yaml network version   # -> 0.74.2
```

Expected:

- Phase advances `ReadyForProvisionerDaemon` -> `PendingNodeUpgrade`.
- `ConfigCRsApplied=True/Succeeded` and `DaemonResult=True/Succeeded`.
- Every per-operation config CR (`<operationId>-<suffix>`) reaches `Valid=True`.
- `consensus-<operationId>.jsonl` contains `ExecuteWorkflowStarted` … `ExecuteWorkflowCompleted`.
- `beacon network version` reports `0.74.2`.

### Failure-path spot checks (optional)

- **Fatal config.** Introduce a CR the operator reconciles to `Valid=False`. Expect
  `DaemonResult=False` with a specific reason (e.g. `ConfigCRInvalid`), the phase held
  at `ReadyForProvisionerDaemon`, the operator marking the CR `Failed`, and no retry.
- **Transient.** Briefly break API access mid-execute. Expect a WARN
  `ExecuteWorkflowRetrying` and recovery on the next watch delivery, with no
  `DaemonResult=False` while inside the handoff deadline (default 60m).
