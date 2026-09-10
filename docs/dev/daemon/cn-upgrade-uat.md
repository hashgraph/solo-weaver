# UAT — basic consensus-node software upgrade via the provisioner-daemon

This runbook validates that the host `solo-provisioner-daemon` performs the HIP-1496
execute phase of a *basic* consensus-node software upgrade (config-only, no infra
upgrade) on a single-node cluster, driven by solo-operator's `beacon` HAPI
control-plane CLI. `beacon` uploads the package and schedules the freeze; the
consensus node, UC sidecar, operator, and the daemon carry out the upgrade after the
freeze fires.

## What this exercises vs. what it does not

Exercised:

- the daemon watches `NetworkUpgradeExecute` and triggers on `ReadyForProvisionerDaemon`;
- it creates the per-operation config CRs from `data/config/` + package root + the
  per-node block-node file, and waits each to `Valid=True`;
- it writes the handshake: `ConfigCRsApplied=True`, `DaemonResult=True`, and advances
  the phase to `PendingNodeUpgrade`;
- it places `manifests/infrastructure-versions.yaml` at the trusted host path (a no-op
  when the package ships none);
- HIP-1496 failure/termination semantics (fatal vs transient, handoff deadline).

Not exercised here (stubs or out of scope):

- external-files download (operator-led `ExternalFiles` CR + Job — solo-operator#1325);
- the runtime safety gate (a no-op for a package with no `infrastructure-versions.yaml`);
- infra upgrade / cluster teardown-reinstall (blocked on daemon self-upgrade, #500).

Keep the upgrade package to config-only changes for a clean run.

## Hard prerequisites

1. **Operator alignment (#1323).** The running operator image must NOT write
   `PendingInfraUpgrade`/`PendingNodeUpgrade` and must drive the terminal off
   `DaemonResult`. Otherwise it races the daemon and the handshake misbehaves.
2. **Daemon-delegated execute mode.** node0 must be provisioned so the UC does not run
   its in-pod provisioner-proxy: the `NetworkUpgradeExecute` CR must halt at
   `ReadyForProvisionerDaemon` and wait for the host daemon. Confirm how this profile
   is selected in the capsule/UC config before starting — the stock docs/beacon e2e is
   the cluster-only path and lets the UC proxy do execute.
3. **Shared upgrade directory.** The daemon reads the extracted package from its
   `upgrade_dir` on the host; the CN pod extracts special file 0.0.150 into that same
   location. On kind this is a hostPath that both the node and the host daemon see.
   Confirm the mount before starting — this is the most fragile part of the local setup.
4. Tooling: `kind`, `docker`, this weaver checkout, and a solo-operator checkout with
   `beacon` (see solo-operator `docs/beacon/README.md`).

Version map (matches docs/beacon): deployed `0.74.0`, upgrade to `0.74.2`.

---

## Step 1 — Build artifacts

Daemon + CLI from this branch:

```bash
task build:cli GOOS=linux GOARCH=amd64
```

This builds the daemon first and stamps its digest into the CLI, producing
`bin/solo-provisioner-linux-amd64` and `bin/solo-provisioner-daemon-linux-amd64`.

Beacon zips + the coupling hash (solo-operator `docs/beacon/README.md` step 1):

```bash
cd <solo-operator>/test/dev
task setup && task build
export ZIP=$PWD/staging/build-v0.74.2.zip
export H=$(shasum -a 384 "$ZIP" | awk '{print $1}')
echo "H=$H"
```

## Step 2 — Cluster + operator + single node at v0.74.0

Follow solo-operator `docs/beacon/README.md` step 2: `task kind:reset`; `task run`
for the operator in its own terminal; `task docker:build:uc` + `kind load`; then
`task deploy:single` (in `docs/example`) for node0 + genesis in namespace
`solo-orbit`.

Deviation for this UAT: provision node0 in **daemon-delegated** mode (prerequisite 2).
Checkpoint node0 is running:

```bash
kubectl -n solo-orbit get pods -l app.kubernetes.io/component=consensus-node
```

## Step 3 — Install and run the host daemon

Provision the daemon-cn ServiceAccount, ClusterRole (the RBAC added in this PR:
`networkupgradeexecutes` get/list/watch, `networkupgradeexecutes/status` patch, and
the 11 config-CR kinds create/get), ClusterRoleBinding, token, and scoped kubeconfig:

```bash
sudo ./bin/solo-provisioner-linux-amd64 daemon service install \
  --components consensus-node --cn-node-id 0 --cn-orbit solo-orbit
```

Sanity-check the grant landed:

```bash
kubectl auth can-i create log4j2configs.operator.solo.hedera.com \
  --as=system:serviceaccount:solo-orbit:solo-provisioner-daemon-cn -n solo-orbit
kubectl auth can-i patch networkupgradeexecutes/status.operator.solo.hedera.com \
  --as=system:serviceaccount:solo-orbit:solo-provisioner-daemon-cn -n solo-orbit
```

Both must print `yes`.

Run the daemon (systemd unit from the install, or foreground for a dev loop), pointed
at the orbit namespace, node 0, and the shared upgrade dir:

```bash
sudo ./bin/solo-provisioner-daemon-linux-amd64 \
  --config /opt/solo/weaver/config/daemon.yaml \
  --node-id 0 --orbit solo-orbit \
  --upgrade-dir /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current \
  --log-level debug
```

With systemd: `sudo systemctl start solo-provisioner-daemon` then
`journalctl -u solo-provisioner-daemon -f`. Confirm it is watching (no CR yet).

## Step 4 — Drive the freeze/upgrade with beacon

Set up the beacon config, payer key, and node0 port-forward per solo-operator
`docs/beacon/README.md` steps 3-4, then run the canonical sequence:

```bash
cd <solo-operator>
./bin/beacon -c /tmp/beacon-solo/config.yaml file upload --zip "$ZIP" --expect-hash "$H"
./bin/beacon -c /tmp/beacon-solo/config.yaml file info
./bin/beacon -c /tmp/beacon-solo/config.yaml freeze prepare --upgrade-zip-hash "$H" --yes
START=$(date -u -v+3M '+%Y-%m-%d.%H:%M:%S' 2>/dev/null || date -u -d '+3 min' '+%Y-%m-%d.%H:%M:%S')
./bin/beacon -c /tmp/beacon-solo/config.yaml freeze upgrade --start-time "$START" --upgrade-zip-hash "$H" --yes
```

`file info` must report a SHA-384 equal to `H`.

## Step 5 — Observe the daemon execute (the point of this UAT)

After the freeze fires the node reaches `FREEZE_COMPLETE`, NMT extracts 0.0.150 into
the upgrade dir, and the UC creates the `NetworkUpgradeExecute` CR and (in
daemon-delegated mode) sets phase `ReadyForProvisionerDaemon`. The host daemon then
takes over.

Watch the daemon-owned phase advance:

```bash
kubectl -n solo-orbit get networkupgradeexecute -w
# ReadyForProvisionerDaemon -> PendingNodeUpgrade
```

Confirm the handshake conditions are both True:

```bash
kubectl -n solo-orbit get networkupgradeexecute -o \
  jsonpath='{range .items[0].status.conditions[*]}{.type}={.status}/{.reason}{"\n"}{end}'
# ConfigCRsApplied=True/Succeeded
# DaemonResult=True/Succeeded
```

Confirm the per-operation config CRs were created and reconciled:

```bash
OP=$(kubectl -n solo-orbit get networkupgradeexecute -o jsonpath='{.items[0].spec.operationId}')
kubectl -n solo-orbit get \
  log4j2configs,nodesettings,applicationproperties,throttlesconfigs,blocknodesconfigs \
  -l operator.solo.hiero.org/operation-id=$OP
```

CR names are `<operationId>-<suffix>`; each should reach `Valid=True`.

Check the daemon's per-operation JSONL audit log (the same milestones appear on the
daemon log channel):

```bash
ls /opt/solo/weaver/*/consensus-$OP.jsonl
# contains ExecuteWorkflowStarted ... ExecuteWorkflowCompleted
```

If the package ships an infrastructure-versions manifest, confirm placement (and the
single rolling backup on a change):

```bash
ls -l /opt/solo/weaver/config/infrastructure-versions.yaml
ls -l /opt/solo/weaver/config/infrastructure-versions.yaml.bak   # only after an overwrite
```

## Step 6 — Verify the new version

After the operator patches the capsule and the pod restarts on the new image:

```bash
cd <solo-operator>
./bin/beacon -c /tmp/beacon-solo/config.yaml network version
kubectl -n solo-orbit get pods -l app.kubernetes.io/component=consensus-node -o wide
```

`network version` should report `0.74.2`.

## Step 7 (optional) — Failure-path spot checks

- **Fatal config.** Add an unrecognised file under the package `data/config/`, or
  arrange a config CR the operator reconciles to `Valid=False`. Expect
  `DaemonResult=False` with a specific reason (e.g. `ConfigCRInvalid`), the phase
  staying at `ReadyForProvisionerDaemon`, the operator marking the CR `Failed`, and no
  retry.
- **Transient.** Briefly break API access mid-execute. Expect a WARN
  `ExecuteWorkflowRetrying` and recovery on the next watch delivery, with no
  `DaemonResult=False` while inside the handoff deadline (default 60m).

## Step 8 — Cleanup

```bash
sudo ./bin/solo-provisioner-linux-amd64 daemon service uninstall --components consensus-node || true
kind delete cluster --name solo-operator
```

---

## Known gaps / confirm before running

- **Daemon-delegated mode** (prereq 2) and the **shared hostPath upgrade dir**
  (prereq 3) are what make this differ from the stock cluster-only beacon e2e. If
  either is not wired for the local single-node setup, the Execute CR is handled by
  the UC proxy instead of the daemon, or the daemon finds no `data/config/`.
- **#1323** must be present in the running operator image, or phase writes race.
- The daemon-cn RBAC is provisioned by `daemon service install` from the rules in
  `internal/workflows/daemon.go`; if you apply RBAC by hand, mirror those exact rules.
