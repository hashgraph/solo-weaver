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

- `--image-pull-secret` is repeatable and host-keyed. A bare `NAME` (default
  `private-registry-creds`) applies to every registry; `HOST=NAME` (e.g.
  `ghcr.io=ghcr-creds`) selects a secret per registry host. Each image (consensus,
  UC, and every multi-registry candidate) resolves its own secret by registry host
  and sets that container's `ImagePullSecrets`; the operator (>= v0.6.0) threads
  them onto the pods and their SAs. Registries come from the manifest — a `HOST=`
  that matches no manifest registry is rejected.
- `--registry-selection-strategy` (`Random` or `Sequential`) sets the emitted
  `SoftwareVersionSource.SelectionStrategy`. Empty uses the weaver default
  (**Sequential** — the manifest's primary registry is tried first, giving
  deterministic failover). Pass `Random` to spread probe traffic. It only has
  effect when the manifest yields multiple registries.
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

## G. Consensus Multi-Registry Support

Validates the multi-registry `SoftwareVersionSource` path end-to-end: weaver reads
multiple registries + layer hashes from the manifest, picks the right pull secret per
registry host, applies a selection strategy (CLI flag > manifest > weaver default
`Sequential`), and the operator (v0.8.0+) selects, verifies, and digest-pins the
image. Covers five variants in one scenario: core multi-registry install, manifest
strategy override, CLI flag beating the manifest, forced failover (decoy at
`registries[0]`), and invalid-strategy rejection.

Pairs with hashgraph/solo-operator#1379, which teaches the `test/dev` deployment-
package producer to emit `images.consensusNode.selectionStrategy` and to insert a
decoy registry at `registries[0]`.

### Prerequisites

- UTM VM running and the usual `.env` with `GITHUB_USER` / `GITHUB_ACCESS_TOKEN`
  (needed for the ghcr chart and the solo-operator Go module).
- `crane` and `jq` on PATH — the operator's `test/dev/Taskfile.yml` uses them to
  compute `rootfs.diff_ids` for the deterministic manifest and to probe the JFrog
  mirror. Install with:
  ```bash
  go install github.com/google/go-containerregistry/cmd/crane@v0.22.0
  ```
- solo-operator checkout at `../solo-operator` with a **clean working tree** —
  `task -d docs/dev/daemon prep`'s dirty-tree guard refuses to switch branches
  otherwise. Stash/commit work-in-progress before running the UAT.
- weaver on a stack that includes operator v0.8.0 (`SOLO_OPERATOR_VERSION = "0.8.0"`
  in `pkg/deps/deps.go`) and the source-only emission + manifest-strategy
  propagation.

### G1. Baseline: multi-registry install with the weaver default (Sequential)

Build the deployment package against the operator PR that emits the multi-registry
manifest, then run the standard runbook:

```bash
# Host:
task -d docs/dev/daemon prep SOLO_OPERATOR_REF=chore/update-beacon-examples
# Verify the staged manifest lists TWO registries + deterministic layer hashes.
cat test/data/build-v0.74.0/manifests/consensus-node-components.yaml

task -d docs/dev/daemon vm:sync

# Inside the VM (via `task vm:ssh:proxy`):
task -d docs/dev/daemon cluster       # kube cluster install
task -d docs/dev/daemon secrets       # creates private-registry-creds in both namespaces
task -d docs/dev/daemon operator      # kube operator install (v0.8.0 chart)
task -d docs/dev/daemon network       # consensus node install
```

Verify the CR on the consensus-node container:

```bash
kubectl -n hiero-network-1 get consensuscapsule node0 -o yaml \
  | yq '.spec.podProperties.containers.consensusNode'
```

Expected:
- `softwareVersion` **absent** (source-only emission).
- `softwareVersionSource.imageRepositories` has two entries (GCR + JFrog), each with
  `imagePullSecrets: [{name: private-registry-creds}]`.
- `softwareVersionSource.imageVerificationSpec` has entries for `linux/amd64` and
  `linux/arm64` with real layer hashes.
- `softwareVersionSource.selectionStrategy: Sequential` (the weaver default).

UC sidecar is unchanged — `softwareVersion` present, no source (single-registry).

Then watch the pod come up and reach `Active`:
```bash
kubectl -n hiero-network-1 get consensuscapsules -w
```

### G2. Manifest `selectionStrategy` is honored

Rebuild with the operator's `CN_SELECTION_STRATEGY` knob so the manifest declares
`Random`:

```bash
task -d docs/dev/daemon prep \
  SOLO_OPERATOR_REF=chore/update-beacon-examples \
  CN_SELECTION_STRATEGY=Random
task -d docs/dev/daemon vm:sync
# Reinstall the node (uninstall first, or use a fresh namespace).
```

```bash
kubectl -n hiero-network-1 get consensuscapsule node0 \
  -o jsonpath='{.spec.podProperties.containers.consensusNode.softwareVersionSource.selectionStrategy}' ; echo
# -> Random
```

### G3. CLI flag beats the manifest

Same package (manifest says `Random`), override on install:

```bash
sudo solo-provisioner consensus node install \
  --namespace hiero-network-1 --node-id 0 --account-id 0.0.3 --profile local \
  --deployment-package-dir /mnt/solo-weaver/test/data/build-v0.74.0 \
  --registry-selection-strategy Sequential
```

```bash
kubectl -n hiero-network-1 get consensuscapsule node0 \
  -o jsonpath='{.spec.podProperties.containers.consensusNode.softwareVersionSource.selectionStrategy}' ; echo
# -> Sequential  (flag beats the manifest's Random)
```

### G4. Forced failover — decoy at `registries[0]`

Insert an unpullable candidate at position 0 and keep Sequential so the operator
must skip past it:

```bash
task -d docs/dev/daemon prep \
  SOLO_OPERATOR_REF=chore/update-beacon-examples \
  CN_DECOY_REGISTRY=artifacts.hashgraph.io/consensus-node-docker-release-local/consensus-node:0.0.0-invalid \
  CN_DECOY_POSITION_0=true
task -d docs/dev/daemon vm:sync
# Reinstall the node.
```

```bash
kubectl -n hiero-network-1 get consensuscapsule node0 -o yaml \
  | yq '.spec.podProperties.containers.consensusNode.softwareVersionSource.imageRepositories'
```

Expected: `[0]` is the decoy (invalid tag), `[1]` is the valid GCR image. The pod
still reaches `Active` — the operator skipped the decoy and pulled from the valid
mirror. Confirm in the operator logs:

```bash
kubectl -n solo-operator logs deploy/solo-operator | grep -iE "registry|probe|skip|fail"
```

### G5. Invalid manifest `selectionStrategy` fails fast

Weaver's manifest parser rejects anything other than `""`, `Random`, `Sequential`
(exact case):

```bash
# Inject a bad value, then try to install:
sed -i.bak 's/version: "0.74.0"/version: "0.74.0"\n    selectionStrategy: random/' \
  test/data/build-v0.74.0/manifests/consensus-node-components.yaml
task -d docs/dev/daemon vm:sync
# Inside the VM:
sudo solo-provisioner consensus node install \
  --namespace hiero-network-1 --node-id 0 --account-id 0.0.3 --profile local \
  --deployment-package-dir /mnt/solo-weaver/test/data/build-v0.74.0
```

Expected: install fails at manifest parse with
`images.consensusNode.selectionStrategy must be one of "", Random, Sequential (got "random")`.
Revert the file afterwards (`mv .../consensus-node-components.yaml.bak ...yaml`).

### G6. Security patch only in a private registry (host-keyed secret must route correctly)

The real-world case this stack is built for: a security patch is published to the
operator's **private** registry; the **public** mirror does not have it yet. Both
registries appear in the manifest. The operator must probe the public one, fail,
fall through to the private one, **authenticate with the pull secret weaver routes
to that host**, verify layer hashes, and deploy.

The scenario stands up an **in-cluster** `registry:2` with basic auth (same shape as
`solo-operator/test/consensus-mock/kind/registry.yaml`, plus htpasswd). Pod → registry
goes via a pinned ClusterIP (`10.96.100.100:5000`); host → registry goes via
`kubectl port-forward` for pushes. One-time containerd insecure-registry allowance
is needed on the VM for kubelet pulls over plain HTTP.

#### G6.1. One-time VM setup — containerd insecure-registry

The in-cluster registry serves plain HTTP; containerd rejects HTTP registries by
default. Inside the VM:

```bash
sudo mkdir -p /etc/containerd/certs.d/10.96.100.100:5000
sudo tee /etc/containerd/certs.d/10.96.100.100:5000/hosts.toml >/dev/null <<'EOF'
server = "http://10.96.100.100:5000"
[host."http://10.96.100.100:5000"]
  capabilities = ["pull", "resolve"]
  skip_verify = true
EOF
# Ensure containerd's config_path points at /etc/containerd/certs.d — most
# distros do by default; verify:
grep -E "config_path\s*=" /etc/containerd/config.toml || \
  echo '!! containerd config_path missing — see "Reset" below for the full edit'
sudo systemctl restart containerd
```

**Reset after you're done** (so other UAT runs are unaffected):
```bash
sudo rm -rf /etc/containerd/certs.d/10.96.100.100:5000
sudo systemctl restart containerd
```

#### G6.2. Deploy the private registry and push the "patch"

Apply the manifest (lives in-repo at `docs/dev/private-registry.yaml`), then push
the consensus-node image under a patch tag. `crane copy` preserves layers byte-for-
byte, so the diff_ids stay deterministic and match the source.

```bash
# Inside the VM:
kubectl apply -f /mnt/solo-weaver/docs/dev/private-registry.yaml
kubectl -n local-registry wait --for=condition=available --timeout=60s deployment/registry

# Port-forward for the host-side push (leave this running in another shell).
kubectl -n local-registry port-forward svc/registry 5001:5000 &
PF_PID=$!

export PATCH_TAG=0.74.0-security.1
crane copy \
  gcr.io/hedera-registry/consensus-node:0.74.0 \
  localhost:5001/consensus-node:$PATCH_TAG \
  --dst-plain-http \
  --dst-auth-username uatuser --dst-auth-password uatpass

# Capture the per-platform diff_ids for the manifest (they'll equal 0.74.0's):
crane config localhost:5001/consensus-node:$PATCH_TAG --platform linux/amd64 \
  --auth-username uatuser --auth-password uatpass --plain-http | jq -r '.rootfs.diff_ids[]'
crane config localhost:5001/consensus-node:$PATCH_TAG --platform linux/arm64 \
  --auth-username uatuser --auth-password uatpass --plain-http | jq -r '.rootfs.diff_ids[]'

kill $PF_PID
```

Create the host-keyed pull secret in the consensus namespace (`--docker-server`
must match the registry host the manifest uses):

```bash
kubectl -n hiero-network-1 create secret docker-registry patch-registry-creds \
  --docker-server=10.96.100.100:5000 \
  --docker-username=uatuser --docker-password=uatpass
```

#### G6.3. Build the base package and hand-edit the manifest

Weaver's test-package producer doesn't synthesize a patch-only-in-one-registry
case, so after a baseline `prep` we replace the generated `consensus-node-components.yaml`:

```bash
task -d docs/dev/daemon prep SOLO_OPERATOR_REF=chore/update-beacon-examples
task -d docs/dev/daemon vm:sync
```

Inside the VM, overwrite
`test/data/build-v0.74.0/manifests/consensus-node-components.yaml` with the diff_ids
captured in G6.2:

```yaml
schemaVersion: 1
images:
  consensusNode:
    version: "0.74.0-security.1"
    selectionStrategy: Sequential
    deterministic:
      supported: true
      layerHashes:
        linux/amd64:
          - "<amd64 diff_id #1>"
          - "<amd64 diff_id #2>"
        linux/arm64:
          - "<arm64 diff_id #1>"
          - "<arm64 diff_id #2>"
    registries:
      # Public mirror listed first — it does NOT have the patch tag, so this is
      # where the operator's probe must fail over.
      - image: "artifacts.hashgraph.io/consensus-node-docker-release-local/consensus-node:0.74.0-security.1"
      # Private in-cluster registry hosting the patch — operator falls through
      # here, auths with patch-registry-creds (host-keyed by weaver), and pulls.
      - image: "10.96.100.100:5000/consensus-node:0.74.0-security.1"
```

#### G6.4. Install and verify

Install, routing the private registry's secret by host and keeping the default
`private-registry-creds` for anything else:

```bash
sudo solo-provisioner consensus node install \
  --namespace hiero-network-1 --node-id 0 --account-id 0.0.3 --profile local \
  --deployment-package-dir /mnt/solo-weaver/test/data/build-v0.74.0 \
  --image-pull-secret private-registry-creds \
  --image-pull-secret 10.96.100.100:5000=patch-registry-creds
```

CR shows both registries with each one's own pull secret:

```bash
kubectl -n hiero-network-1 get consensuscapsule node0 -o yaml \
  | yq '.spec.podProperties.containers.consensusNode.softwareVersionSource'
```

Expected:
- `imageRepositories[0]`: `artifacts.hashgraph.io/...:0.74.0-security.1`, no secret
  (unmapped public host; the bare default `private-registry-creds` applies by host
  rule — won't match the public host either, so pulls are anonymous).
- `imageRepositories[1]`: `10.96.100.100:5000/...` with
  `imagePullSecrets: [{name: patch-registry-creds}]`.
- `selectionStrategy: Sequential`.
- `imageVerificationSpec`: the two platforms with the matching diff_ids.

Pod reaches `Active`. The operator logs show the public probe 404'ing and the
private pull succeeding:

```bash
kubectl -n solo-operator logs deploy/solo-operator | grep -iE "registry|probe|skip|pulled|auth"
```

The pulled image on the pod is pinned to the private registry's digest:

```bash
kubectl -n hiero-network-1 get pod node0-consensus-0 \
  -o jsonpath='{.spec.containers[?(@.name=="consensus-node")].image}' ; echo
# -> 10.96.100.100:5000/consensus-node@sha256:...
```

**Negative check** — delete the secret and reinstall; the pull must fail:
```bash
kubectl -n hiero-network-1 delete secret patch-registry-creds
# ... re-run `consensus node install` with the same flags ...
kubectl -n hiero-network-1 describe pod node0-consensus-0 | grep -A3 -i "fail\|unauth"
# -> ErrImagePull / 401 Unauthorized on 10.96.100.100:5000
```
Recreate the secret to recover.

#### G6.5. Teardown

```bash
kubectl -n hiero-network-1 delete secret patch-registry-creds --ignore-not-found
kubectl delete -f /mnt/solo-weaver/docs/dev/private-registry.yaml
# Then the containerd reset from G6.1.
```

### Known gaps

- `TestResolveDaemonBinarySource` in `cmd/cli/commands/block/node` can fail in the
  VM for environment-dependent reasons unrelated to this scenario; ignore it in
  the e2e output.

---

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
| G1–G5. Multi-Registry Support | ~20 min | VM, ghcr PAT, `crane`+`jq`, operator PR #1379 branch |
| G6. Security patch in a private registry only | ~15 min | G1–G5 prereqs + Docker in VM for `registry:2` |
