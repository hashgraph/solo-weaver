# Acceptance Tests

Manual runbook for end-to-end validation of solo-weaver workflows. Each scenario can be run
independently, but they are listed in a logical order. All commands assume a UTM VM environment
on macOS.

## Running

**From inside the VM** (after `task vm:ssh:proxy`):
```bash
task uat:lifecycle  # Core lifecycle: setup → core upgrades → teardown (CI-friendly, no Docker)
task uat:compat     # Backward compatibility (standalone, installs released version first)
task uat:all        # Everything: lifecycle + teleport + alloy + compat (requires Docker)

# Individual steps (for debugging or manual runs):
task uat:setup         # Install cluster + block node (v0.26.0)
task uat:core          # Block node upgrades + reset (requires setup)
task uat:purge-storage # Verify the three uninstall variants: (no flag) / --with-reset / --purge-storage (requires setup; destructive — leaves no block node behind)
task uat:teleport      # Teleport install/uninstall (requires setup + Docker)
task uat:alloy         # Alloy install/uninstall (requires setup + Docker)
task uat:teardown      # Uninstall everything
```

These tasks are designed to run inside any Linux VM — both the local UTM VM
(via `task vm:ssh:proxy`) and CI QEMU VMs (via the `zxc-uat-test.yaml` workflow).

## Prerequisites

- UTM VM set up (`task vm:start`)
- Cache proxy running (`task proxy:start`)
- Binary built (`task build:cli GOOS=linux GOARCH=arm64`)
- Host tree synced into the VM (`task vm:sync`) — required after editing files on the host or switching worktrees;
  the `vm:test:*`, `vm:alloy:*`, `vm:teleport:*`, and debug tasks invoke this automatically, but the in-VM UAT runs
  driven from `task vm:ssh:proxy` rely on the most recent manual `task vm:sync`.

## A. Core Block Node Lifecycle

Tests the primary user journey: install → upgrade → reset → uninstall.

### Setup

```bash
# On macOS:
task vm:reset                              # Clean VM state
task build:cli GOOS=linux GOARCH=arm64     # Build latest binary
task proxy:start                           # Ensure proxy is running
task vm:ssh:proxy                          # SSH with proxy tunnels
```

### Steps

```bash
# Inside the VM:

# 1. Self-install
cd /tmp && cp /mnt/solo-weaver/bin/solo-provisioner-linux-arm64 .
sudo ./solo-provisioner-linux-arm64 install

# 2. Block node install (pins version 0.26.0 via config)
sudo solo-provisioner block node install -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
kubectl get pods -n block-node          # Pods running
kubectl get pods -n block-node -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.image}{end}{"\n"}{end}'  # Image version
kubectl get pv                          # PVs created
solo-provisioner version                # CLI version matches build
```

```bash
# 3. Upgrade to intermediate version (v0.29.0)
sudo solo-provisioner block node upgrade -p local \
  --chart-version 0.29.0 \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
helm list -n block-node                 # Chart version is 0.29.0
kubectl get pods -n block-node -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.image}{end}{"\n"}{end}'  # Image shows 0.29.0
```

```bash
# 4. Upgrade to intermediate version (v0.30.2 — explicit flag)
sudo solo-provisioner block node upgrade -p local \
  --chart-version 0.30.2 \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
helm list -n block-node                 # Chart version is 0.30.2
kubectl get pods -n block-node -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.image}{end}{"\n"}{end}'  # Image shows 0.30.2
```

```bash
# 5. Upgrade to latest (v0.35.1 — version from config file, no CLI flag)
sudo solo-provisioner block node upgrade -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy_latest.yaml
```

**Verify:**
```bash
helm list -n block-node                 # Chart version is 0.35.1
kubectl get pods -n block-node -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{range .spec.containers[*]}{.image}{end}{"\n"}{end}'  # Image shows 0.35.1
```

```bash
# 6. Reset block node (clears storage, keeps cluster)
sudo solo-provisioner block node reset -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
kubectl get pods -n block-node          # Pods running (recreated)
```

```bash
# 7. Block node uninstall
sudo solo-provisioner block node uninstall -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
kubectl get pods -n block-node          # No pods (or namespace gone)
helm list -n block-node                 # No release
```

```bash
# 7. Kubernetes cluster uninstall
sudo solo-provisioner kube cluster uninstall --continue-on-error
```

**Verify:**
```bash
kubectl get nodes 2>&1                  # Should fail (no cluster)
```

```bash
# 8. Self-uninstall
sudo solo-provisioner uninstall
```

**Verify:**
```bash
which solo-provisioner 2>&1             # Not found
```

---

## B. Teleport Lifecycle

Tests teleport node agent and cluster agent install/uninstall.

### Prerequisites

Block node must be installed (run scenario A steps 1-2 first, or at minimum have a Kubernetes
cluster running).

### Cluster Agent

```bash
# 1. Install cluster agent
sudo solo-provisioner teleport cluster install \
  --values=/mnt/solo-weaver/test/teleport/teleport-values-local.yaml
```

**Verify:**
```bash
kubectl get pods -n teleport-agent      # Pods running
helm list -n teleport-agent             # Release present
```

```bash
# 2. Uninstall cluster agent
sudo solo-provisioner teleport cluster uninstall
```

**Verify:**
```bash
helm list -n teleport-agent             # No release
```

### Node Agent

> Note: Requires a running Teleport proxy server. For local testing, set up Teleport
> via `task vm:teleport:start` first. See Taskfile for details.

```bash
# 3. Install node agent (replace token/proxy with your values)
sudo solo-provisioner teleport node install \
  --token=<join-token> \
  --proxy=<proxy-addr>:3080
```

**Verify:**
```bash
sudo systemctl status teleport          # Active and running
cat /opt/solo/weaver/state/state.yaml | grep -A3 teleportState  # nodeAgent.installed: true
```

```bash
# 4. Uninstall node agent
sudo solo-provisioner teleport node uninstall
```

**Verify:**
```bash
sudo systemctl status teleport 2>&1     # Not found or inactive
cat /opt/solo/weaver/state/state.yaml | grep -A3 teleportState  # nodeAgent.installed: false
```

---

## C. Alloy Lifecycle

Tests Grafana Alloy observability stack install/uninstall.

### Prerequisites

Kubernetes cluster must be running (from scenario A).

```bash
# 1. Install Alloy (no remotes — simplest config)
sudo solo-provisioner alloy cluster install \
  --cluster-name=uat-test
```

**Verify:**
```bash
kubectl get pods -n grafana-alloy       # Pods running
helm list -n grafana-alloy              # grafana-alloy release present
```

```bash
# 2. Uninstall Alloy
sudo solo-provisioner alloy cluster uninstall
```

**Verify:**
```bash
helm list -n grafana-alloy              # No releases
```

---

## D. Proxy Verification

Validates that the cache proxy is correctly routing and caching traffic.

### Setup

```bash
# On macOS:
task proxy:stop
# Remove cached data to start fresh:
docker volume rm cache-proxy_cache-data cache-proxy_registry-data cache-proxy_goproxy-cache 2>/dev/null; true
task proxy:start
```

### Steps

```bash
# On macOS (separate terminal) — watch traffic:
docker exec solo-weaver-cache-proxy tail -f /var/log/squid/access.log
```

```bash
# On macOS:
task vm:ssh:proxy

# Inside VM:
# 1. First run — downloads go through proxy (TCP_MISS)
sudo solo-provisioner block node install -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify (in Squid log):** Lines showing `TCP_MISS` for download URLs (cdn.dl.k8s.io, get.helm.sh, etc.)

```bash
# 2. Reset and reinstall — should hit cache (TCP_HIT)
sudo solo-provisioner kube cluster uninstall --continue-on-error
sudo solo-provisioner block node install -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify (in Squid log):** Lines showing `TCP_HIT` for the same URLs.

```bash
# On macOS:
task proxy:status                       # Shows cache sizes and hit ratios
```

---

## E. Backward Compatibility

Tests upgrading the CLI binary while preserving state from a prior version.

### Setup

```bash
# On macOS:
task vm:reset
task vm:ssh:proxy
```

### Steps

```bash
# Inside VM:

# 1. Install a released version (download from GitHub releases)
curl -sSL https://raw.githubusercontent.com/hashgraph/solo-weaver/main/install.sh | bash

# 2. Deploy block node with the released version
sudo solo-provisioner block node install -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

**Verify:**
```bash
solo-provisioner version                # Shows released version
kubectl get pods -n block-node          # Running
cat /opt/solo/weaver/state/state.yaml   # State file exists with current schema
```

```bash
# 3. Upgrade CLI to the new binary (from current branch)
cd /tmp && cp /mnt/solo-weaver/bin/solo-provisioner-linux-arm64 .
sudo ./solo-provisioner-linux-arm64 install
```

**Verify:**
```bash
solo-provisioner version                # Shows new version (0.0.0 for dev builds)
```

```bash
# 4. Run an upgrade — should trigger startup migrations if needed
sudo solo-provisioner block node upgrade -p local \
  -c /mnt/solo-weaver/test/config/config_with_proxy_latest.yaml
```

**Verify:**
```bash
kubectl get pods -n block-node          # Running with updated version
cat /opt/solo/weaver/state/state.yaml   # State file version matches current schema
# Check logs for migration messages:
grep -i migration /opt/solo/weaver/logs/solo-provisioner.log
```

---

## F. Consensus Node Deployment

End-to-end deployment of a Hedera consensus network — a single node (F1) or a
multi-node network (F2) — via the solo-operator. The
solo-operator chart and its images (consensus-node, UC sidecar) are hosted in the
**private** `ghcr.io/hashgraph/solo-operator/*` registry, so two credential steps
are required — one for Helm (pulling the operator *chart*) and one for Kubernetes
(pulling the *images*).

### Prerequisites

- UTM VM set up and built: `task vm:ssh` then `task uat:rebuild`
- `GITHUB_USER` and `GITHUB_ACCESS_TOKEN` in a root `.env` file (see `.env.example`)
  — classic PAT with `read:packages`, SSO-authorized for the `hashgraph` org.

### Common setup (single- and multi-node)

**Order matters.** Cluster install is workload-agnostic and installs **no**
operator. The operator is a separate step that needs private-registry credentials
in place first:
- Helm registry login (chart pull) is host-side, no cluster needed.
- The operator-namespace pull secret must exist *before* `kube operator install`.
- The consensus-namespace secrets must exist *before* `consensus node install`.

These four steps are identical whether you deploy one node or many — only the
install/genesis steps in F1/F2 below differ.

```bash
# Pick a namespace (also the Orbit name). Use a distinct one per network in a
# shared cluster: hiero-network-1, hiero-network-2, ...
export NS=hiero-network-2
export PKG=./test/data/build-v0.74.2   # extracted HIP-1494 deployment package

# 1. Helm auth for the private operator chart (no cluster needed; run once per VM).
task uat:registry:login:ghcr

# 2. Install the cluster only (no operator). --node-type sizes the host with --profile.
sudo solo-provisioner kube cluster install --profile local --node-type consensus

# 3. Create ALL secrets in one shot: the ghcr pull secret in both the operator and
#    consensus namespaces, plus the consensus gossip/gRPC secrets for node0..node4.
#    (Same registry, one credential; NODE= unset means all nodes.)
task uat:secrets NS="${NS}"

# 4. Install the solo-operator (pulls private images via --image-pull-secret, default private-registry-creds).
sudo solo-provisioner kube operator install
```

> **Consensus commands are gated.** The whole `consensus` group is hidden and
> refuses to run unless you opt in with `--experimental` (shown below) or
> `SOLO_PROVISIONER_ENABLE_CONSENSUS=1` — it is not production-ready. Under `sudo`
> use the flag; sudo drops the environment, so the env var would not reach the process.

### F1. Single consensus node

Install one node, then generate genesis. With defaults it is `node-id 0` /
`account-id 0.0.3`; its secrets (`node0-gossip-keys`, `node0-grpc-tls-keys`) were
created by step 3.

```bash
# 5. Create the ConsensusCapsule (non-blocking; reports status, does not wait for Active).
sudo solo-provisioner consensus node install \
  --experimental \
  --namespace "${NS}" \
  --profile local \
  --deployment-package-dir "${PKG}"

# 6. Apply genesis and wait for the node to reach Active (discovery — roster from the one capsule).
sudo solo-provisioner consensus network genesis --experimental --namespace "${NS}"
```

### F2. Multi-node consensus network

There is no node-count flag: **run `consensus node install` once per node into the
same namespace**, each with a distinct `--node-id` and `--account-id`, then run
`consensus network genesis` **once**. In discovery mode the operator builds the
roster from *all* ConsensusCapsules in the namespace. Account IDs follow the Hedera
convention `0.0.(3 + node-id)`. Step 3 already created secrets for `node0..node4`.

```bash
# 5. Install N nodes (node0..node3 -> accounts 0.0.3..0.0.6) into the SAME namespace.
for i in 0 1 2 3; do
  sudo solo-provisioner consensus node install \
    --experimental \
    --namespace "${NS}" \
    --profile local \
    --node-id "${i}" \
    --account-id "0.0.$((3 + i))" \
    --deployment-package-dir "${PKG}"
done

# 6. One genesis for the whole network — operator discovers all 4 capsules.
sudo solo-provisioner consensus network genesis --experimental --namespace "${NS}"
```

Expected (both F1 and F2):
- Cluster install completes without touching the operator/registry.
- `kube operator install` reaches *Installing Solo Operator* and succeeds; the
  operator pod pulls its private image (`kubectl -n solo-operator get pods`).
- Consensus pods pull the private images with no ServiceAccount patching:
  ```bash
  kubectl -n "${NS}" get pod "${NS}-consensus-0" \
    -o jsonpath='{.spec.imagePullSecrets}'   # -> [{"name":"private-registry-creds"}]
  ```
- After genesis, every node transitions to `platformStatus: ACTIVE`. For the
  multi-node network, confirm the full roster is present and healthy:
  ```bash
  kubectl -n "${NS}" get consensuscapsules   # -> N capsules, all Active
  ```

### Notes / gotchas

- `--image-pull-secret` (default `private-registry-creds`) sets `SoftwareVersion.ImagePullSecrets`
  on the capsule; the operator (>= v0.6.0) threads it onto the pods and their SAs.
- Genesis precedence: `--genesis-file` > `--deployment-package-dir` > discovery. Use
  discovery genesis when the packaged genesis pins IP gossip endpoints that do not
  match the pod IPs.
- **Multi-node:** for an in-cluster discovery network, do **not** pass
  `--deployment-package-dir` to `consensus network genesis` — that applies the
  package's single-node `genesis-network.json` verbatim and bypasses roster
  discovery. Leave it off so the operator builds the roster from the capsules.
- **Max nodes = 5** (`node0..node4`) with the sample secrets from `task uat:secrets`.
  In discovery mode the gossip keys come from those K8s secrets (not the package's
  `data/keys/*.pem`), so more nodes need additional gossip/gRPC secrets.
- Identity flags (`--ledger-id`, `--chain-id`, `--image-repo`/`--image-tag`) are
  extracted from the deployment package, so they stay identical across nodes and do
  not need to be repeated per node.

---

## Full VM tests in pull request CI

The VM jobs (integration tests and the four UAT scenarios) no longer run on every push to a pull request. A push runs build and unit tests in PR Checks, plus a separate **Full VM Tests Gate** check (`flow-full-vm-gate.yaml`).

The gate passes in one of two ways:

- One run of the **Full VM Tests** workflow has passed all five jobs for the PR's current head commit.
- The PR has the `skip-kvm-runner` label.

Run the full set on the branch to validate, normally the top PR of a stack:

```bash
gh workflow run flow-test-full.yaml --ref <branch>
```

Or use Actions > Full VM Tests > Run workflow and pick the branch. It runs the same five jobs the PR workflow used to run (integration, `compat`, `lifecycle`, `purge-storage`, `plugin-preset-upgrade`). When they all pass, the workflow re-runs the gate job by itself.

### Finding which PR in a stack broke

If the full run on the top PR fails:

1. Use "Re-run failed jobs" on the run. This re-runs only the red jobs on the same commit and rules out a flaky test.
2. If it still fails, bisect: run the workflow on a PR in the middle of the stack. A pass means the break is above it, a failure means it is at or below it. This takes about log2 of the stack size in runs.
3. To re-test only what failed, untick the other jobs in the run form (or pass `-f run-compat=false` and so on on the command line):

   ```bash
   gh workflow run flow-test-full.yaml --ref <branch> \
     -f run-integration=false -f run-lifecycle=false \
     -f run-purge-storage=false -f run-plugin-preset-upgrade=false
   ```

   That runs only `compat`. The inputs are `run-integration`, `run-compat`, `run-lifecycle`, `run-purge-storage` and `run-plugin-preset-upgrade`, all `true` by default. At least one must be selected: with all five switched off, the run fails at once in a `Validate inputs` job instead of ending green having tested nothing.

A run with any job switched off is for diagnosis only. It does not satisfy the gate and does not re-run it. The gate needs one run in which all five jobs passed; results from different runs are not combined. To clear a gate after a failure, use "Re-run failed jobs" on the full run, not a separate partial run. When all five jobs pass, the workflow re-runs the gate by itself.

Notes:

- The gate is red on a ready PR until the full run has passed. Draft PRs and docs-only changes (only `*.md` files and `docs/`) pass it without a run.
- Adding or removing the `skip-kvm-runner` label re-checks the gate by itself within seconds, and so does marking a PR ready or converting it to a draft. The gate reads the PR's current labels, draft state and commit from the API, so it never works from stale information. Build and unit tests are not restarted.
- If the gate result looks out of date, close and reopen the PR or push a commit. Re-running an old gate run is also safe, because it reads the live state.
- A passing run covers one commit. Any later push needs a new run.
- For a stack, run it on the top PR and label the PRs below it with `skip-kvm-runner`. Rebase or merge each parent's latest commit into its child and push first, so the top PR contains everything beneath it.
- A passing top PR does not prove each lower PR on its own. For infrastructure changes, run `Test UAT` or `Test Integration Setup` by hand on the lower PR.
- Starting a new full run on a branch cancels an older full run still in progress. A partial run (any job switched off) never cancels a full run, and the other way round.
- To run a single UAT scenario or a single test, use the `Test UAT` and `Test Integration Setup` workflows.

## Quick Reference

| Scenario | Duration | Dependencies |
|----------|----------|-------------|
| A. Core Lifecycle | ~10 min | VM, proxy |
| B. Teleport | ~5 min | Kubernetes cluster |
| C. Alloy | ~3 min | Kubernetes cluster |
| D. Proxy Verification | ~15 min | VM, proxy (fresh caches) |
| E. Backward Compat | ~15 min | VM, proxy, released binary |
| F1. Single Consensus Node | ~15 min | VM, ghcr PAT (private registry) |
| F2. Multi-node Consensus Network | ~20 min | VM, ghcr PAT (private registry) |
