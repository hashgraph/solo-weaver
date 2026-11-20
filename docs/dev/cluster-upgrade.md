# Cluster upgrade

How `kube cluster upgrade` moves a single-node cluster to the Kubernetes and Cilium versions this binary
installs: how the plan is built, what each step (a *hop*) does, and what it records. The command's flags
and output are in [the command guide](../commands/README.md#upgrade-experimental).

The code is in `internal/bll/clusterupgrade` (plan and executors), `internal/workflows/cluster_upgrade.go`
(one workflow per hop kind) and `internal/workflows/steps` (the steps).

## A run in short

1. Take the `system` and `cluster` state locks, and hold them to the end.
2. Read the live cluster and plan every hop to the target.
3. Ask the operator to confirm, unless `--force`, `--non-interactive` or no terminal.
4. Run the next hop. With `--all`, plan again from the live cluster and run the next one, until the plan is
   empty.
5. At the target, record the cluster and the host binaries.

`--plan` stops after step 2. `--dry-run` also runs the next hop's pre-hop checks. Neither takes a lock or
changes anything.

## The plan

### What it reads

- **The state record and the catalog**, then **the probe**. With no kubeconfig, or when the API server does
  not answer, the plan has one `not-observed` blocker that names the cause, and nothing is read from the
  cluster. Without the probe, the cluster checker would hand back the recorded cluster and the plan would
  come from the record.
- **The live cluster and host:** the API server and kubelet versions, the container runtime, the node
  count, the Cilium chart and agent, and every catalog chart's Helm release (version, chart name, status).
- **The state record**, only to tell what went away: a chart whose release is gone keeps the identity it
  was recorded with. Live values decide everything else.

A chart's *identity* is current when its release runs the chart the catalog manages (the last part of the
catalog's chart reference, such as `prometheus-node-exporter`), and legacy when it runs another chart under
the same release name, such as the Bitnami `node-exporter`.

### Hop kinds

| Kind | Step | What it does |
|---|---|---|
| Cilium | `patch` | Cilium to the catalog version of its minor |
| Cilium | `minor` | Cilium one minor up |
| Kubernetes | `patch` | Kubernetes to the catalog version of its minor |
| Kubernetes | `minor` | Kubernetes one minor up, to that minor's catalog version |
| Kubernetes | `resume` | Bring a kubelet that a stopped hop left behind up to the control plane |
| Structural | `replace` | Replace the Bitnami node-exporter with the prometheus-community chart |
| Structural | `migrate` | Move External Secrets from its 0.x chart to the catalog default |
| Charts | `coupled` | Raise the charts a Kubernetes minor needs to that minor's minimum |
| Charts | `refresh` | Raise every chart below its catalog default to the default |

The catalog version of a Kubernetes minor is its latest patch in `pkg/software/catalog/host-k8s-<minor>.yaml`.

### Order

1. **Resume:** a kubelet behind the control plane (same minor, or one minor behind) is caught up first.
2. **Structural** hops, node-exporter before External Secrets.
3. **Coupled** bump for the minor the cluster is on, when its charts are older than that minor needs.
4. **Cilium patch** to its minor's catalog version.
5. **The ladder**, one Kubernetes minor at a time, up to the target. For each minor:
   - Cilium moves first, one minor at a time, until it runs on the next Kubernetes minor. Each Cilium step
     must run on the minor the cluster is still on.
   - A Kubernetes patch to the minor's catalog version, if the cluster is below it.
   - The coupled bump for the next minor, just before it.
   - The Kubernetes minor hop, which lands on the next minor's catalog version.
6. A Kubernetes patch to the target, if the last minor is below it.
7. **Refresh**, only when the target is the default, last.

`--to 1.35` stops the ladder at that minor's catalog version; `--to 1.35.10` must be that version. A target
below the installed minor or past the default is refused.

A v0.35 host (Kubernetes 1.33.4, Cilium 1.18.1, MetalLB 0.15.2) plans:

```text
Cilium      1.18.1  -> 1.18.16  (patch)
Cilium      1.18.16 -> 1.19.10  (minor)   1.18 does not run on 1.34
Kubernetes  1.33.4  -> 1.33.13  (patch)
Kubernetes  1.33.13 -> 1.34.13  (minor)
Cilium      1.19.10 -> 1.20.2   (minor)   1.19 does not run on 1.35
Kubernetes  1.34.13 -> 1.35.10  (minor)
metallb     0.15.2  -> 0.16.1   (needed by Kubernetes 1.36)
Kubernetes  1.35.10 -> 1.36.5   (minor)
```

plus the structural hops and the refresh when the host runs the legacy charts and older chart versions.

### What blocks a plan

A blocked plan has no hops, lists every blocker, and the command exits non-zero without changing anything.

| Code | When |
|---|---|
| `not-observed` | No kubeconfig, the API server did not answer, or part of the cluster could not be read |
| `multiple-nodes` | More than one node |
| `kubernetes-unknown` | The API server or kubelet version is missing or not a release version |
| `kubernetes-not-in-ladder` | The Kubernetes minor has no catalog file |
| `kubelet-skew` | The kubelet is ahead of the control plane, or more than one minor behind |
| `cilium-unknown` | No Cilium release, no agent, or a chart version that is not a release version |
| `cilium-mismatch` | The Cilium chart and agent versions differ |
| `cilium-not-in-ladder` | The Cilium minor is not in the catalog |
| `unsupported-pairing` | The running Cilium does not support the running Kubernetes minor |
| `chart-unknown` | A chart's identity or version cannot be read |
| `no-migration` | A chart runs a legacy chart and no structural hop replaces it |
| `release-not-deployed` | A chart's last Helm revision is failed or pending: roll it back or finish it, then plan again |

A chart whose release is gone is skipped, except a node-exporter swap that stopped after its uninstall:
the record still names the legacy chart, so the replace hop is planned again from the recorded version.

## The matrix

`MatrixFromCatalog` builds the resolver's view of the catalog: one Kubernetes rung per minor (its catalog
version and the chart minimums it needs), one Cilium rung per minor (its version and the Kubernetes minors
it supports), the charts' versions and defaults, and the default Kubernetes. It calls `Validate()`.

`Validate()` checks that the rungs are ascending with no gap, that every version is a release version, that
the defaults and minimums are listed versions, and two walks:

1. **Every Kubernetes step has a Cilium.** For each minor below the default, some Cilium rung runs on both
   that minor and the next.
2. **Every start can walk there.** For each Kubernetes minor below the default, and each Cilium rung that
   runs on it, the Cilium steps of the ladder reach the default. This is how a cluster can start, so a
   valid matrix never leaves a cluster without a next hop.

The first walk is not enough on its own. Take Cilium 1.18 (Kubernetes 1.30 to 1.33), 1.19 (1.35 only) and
1.20 (1.33 to 1.36), with Kubernetes 1.33 to 1.36:

- Cilium 1.20 runs on both minors of every Kubernetes step, so walk 1 passes.
- A cluster on Kubernetes 1.33 with Cilium 1.18 must move Cilium before Kubernetes 1.34. Cilium moves one
  minor at a time, and 1.19 does not run on 1.33, where the cluster still is. It has no next hop.

Walk 2 catches it and names the start (the message, without its error-type prefixes):

```text
a cluster on Kubernetes 1.33 with Cilium 1.18 cannot reach Kubernetes 1.36,
cause: common.illegal_state: no Cilium step from 1.18.16 runs on both Kubernetes 1.33 and 1.34
```

A bad catalog range fails `TestMatrixFromCatalog` with this error, and the same error makes `--plan` fail
with reason `Internal`. Fix the range in `pkg/software/catalog/cilium.yaml` against Cilium's tested list;
see [catalog-updates.md](catalog-updates.md).

## Executors

`ApplyHop` sends a hop to the executor of its kind: `ApplyCiliumHop`, `ApplyKubernetesHop`,
`ApplyStructuralHop` or `ApplyChartsHop`. Each takes the same path:

1. **Locks.** The caller holds the locks in a `state.HeldLocks`. The executor refuses locks that do not
   cover what it writes, before it reads anything. It never locks itself: a second acquire of a held flock
   fails, even in the same process.
2. **Read.** The live cluster, as the plan reads it. A cluster the probe cannot reach is refused with the
   cause and what to do.
3. **Done?** When the cluster already runs the hop's target, it does nothing and records nothing.
4. **Checks.** Every pre-hop check runs; any failure refuses the hop and changes nothing.
5. **One workflow:** the hop's steps, then a node check (`CheckClusterNodesReady`), then a wait of up to
   2 minutes until the whole cluster can be read (`WaitClusterReadable`), then `RecordState`. A write that
   fails is a warning on a hop that worked, and the entry is marked NotPersisted. The cluster is never
   recorded while it is still settling.
6. **One `upgrade` / `cluster` entry** in the action history per hop, failed or not, naming the files
   written.

Every step checks the live state first, so a re-run of a hop that stopped part way skips the steps already
done. The done check looks only at versions, though: a hop killed after its versions moved but before its
last steps is taken as done. A Kubernetes hop killed before its uncordon leaves the node cordoned (lift it
with `kubectl uncordon`), and a Cilium hop killed after `cilium upgrade` does not check its settings again. No hop rolls back: Kubernetes cannot be downgraded, and going back on Cilium or a chart is
a decision for a person. The failed step's hints say how.

| Executor | Locks it needs | Records | Entry inputs |
|---|---|---|---|
| Cilium | `system`, `cluster` | `cluster.yaml`; the cilium CLI in `system.yaml` | `component`, `from`, `to`, `cilium-cli` |
| Kubernetes | `system`, `cluster` | `cluster.yaml`; kubeadm, kubelet, kubectl and cri-o in `system.yaml` | `component`, `step`, `from`, `to`, `cri-o` |
| Structural | `cluster` | `cluster.yaml` | `component`, `step`, `from`, `to` |
| Charts | `cluster` | `cluster.yaml` | `component`, `step`, `charts`, and `kubernetes` for a coupled hop |

Host binary versions are recorded by hashing the binaries, not from the hop's intent. Other commands check
a binary against its recorded version, so a stale record makes them refuse it. A hop killed before
`RecordState` leaves its records stale; a hop taken as done records nothing, and only Kubernetes hops
record the Kubernetes binaries, so the record is fixed when a run reaches the target.

### Cilium

**Checks:** the cluster was read; one node; Cilium is where the hop starts; the agent matches its chart;
the target is a catalog version; one step at a time (a patch stays in its minor, a minor moves one up);
the target runs on this Kubernetes.

**Done** when the chart and the agent run the target.

**Steps:**

1. Put the cilium CLI on the catalog default (a v0.35 host has 0.18.7). Skipped when it is already there.
2. Check Cilium is healthy at the start version.
3. Fetch the target chart with its checksum verified, and re-render `cilium-config`.
4. Run Cilium's pre-flight DaemonSet so the node pulls the new images, then remove it. It gets Cilium's
   documented pre-flight values plus our API server address, not our full values: those would put Hubble
   relay and its certificates in the pre-flight release, which the `cilium` release owns.
5. `cilium upgrade`, pinned to the target version and the verified chart directory. The CLI binary is
   checked against the hop's CLI version, not its record, which the workflow updates only at its end.
6. Wait until every agent runs the target, the operator is available, and `enable-host-legacy-routing`
   and `bpf-lb-acceleration` keep our values. Then re-run the bandwidth-manager guard.

**On failure:** the CLI is recorded if the hop had already replaced it. The `cilium upgrade` failure hint
gives the command to go back to the start version.

### Kubernetes

**Checks:** the cluster was read; one node; Kubernetes is where the hop starts (a hop that already moved
the control plane may run again); the target is a catalog version; one step at a time; the running Cilium
supports the target; the charts the target needs are installed.

**Done** when the control plane and kubelet run the target and the runtime is the target's cri-o: the
latest catalog cri-o of the target minor.

**Steps:**

1. Check the node and control plane are healthy, waiting up to 2 minutes for a control plane that just
   restarted.
2. Download and verify every target binary before changing anything.
3. Upgrade kubeadm, check the binary against the target, and run `kubeadm upgrade apply`. The apply is
   skipped only when every control-plane component runs the target, so an apply that stopped part way runs
   again.
4. Cordon the node, with an annotation that marks the cordon as the hop's. There is no drain: on one node,
   evicted pods have nowhere to go.
5. Stop the kubelet, then cri-o. Swap cri-o, the kubelet and kubectl. Start cri-o, restart the kubelet. A
   configured pull-through cache gets its `registries.conf` back.
6. Wait until the node, kubelet, runtime and control-plane pods run the target, and no kube-proxy
   DaemonSet exists.
7. Lift the hop's cordon. An operator's cordon stays.

**cri-o:** the previous release is moved to `sandbox/backup/cri-o-before-<version>` before the install:
binaries are moved, not overwritten, because every running container's `conmon` holds its binary open.
When the install or configure fails, the previous release is moved back, so the node keeps a container
runtime. When it works, the old binaries are dropped and the old config files stay in the backup, with a
warning: the new release's config files are installed and `Configure` applies only our settings again.

**On failure:** the workflow's failure handler puts back a configured pull-through cache's
`registries.conf`, reloads systemd, starts cri-o and the kubelet, and lifts the hop's cordon. It runs on a
context of its own with a time limit, so it also runs after a Ctrl-C. kubeadm rolls back its own failed
apply. The host binaries are hashed and recorded in the hop's entry. A re-run continues: the next plan may
start with a resume hop.

### Structural

**Checks:** the cluster was read; one node; the chart is the legacy one the hop starts at; its release is
deployed; the target is the catalog default. A node-exporter swap whose release is gone passes only when
the record still names the legacy chart at the hop's start.

**Done** when the chart runs the target under its current identity, with a deployed release.

**node-exporter replace:**

1. Check the `ServiceMonitor` CRD exists (the new chart creates one; `alloy cluster install` installs the
   CRD), and download and verify the prometheus-community chart. Either failure changes nothing.
2. Uninstall the Bitnami release and wait for its pods to go: both charts bind host port 9100.
3. Install the new chart under the same release name and namespace, with the values new installs use.
4. Wait until the DaemonSet is ready and exactly one ServiceMonitor matches Alloy's selector.

**External Secrets migrate:**

1. Find the one ESO release, in whatever namespace `eso operator install` put it. Note which
   ExternalSecrets are Ready, and download the target chart.
2. `helm upgrade` with our values, waiting for the rollout.
3. Wait until the controllers run the target image and every ExternalSecret that was Ready is Ready again.

This is a chart upgrade, not a resource conversion: the 0.20.2 and 2.13.0 charts declare the same API
versions on every ESO CRD (`v1` stored, `v1beta1` listed but not served), and we use only `v1`.

**On failure:** the hints name `alloy cluster install` (node-exporter) and `helm rollback` (ESO). A swap
that stopped after its uninstall is planned again.

### Charts

**Checks:** the cluster was read; one node; every chart runs its hop start, or already its target (so a hop
that stopped part way can run again); every target is the catalog's (the default for a refresh, the
minor's minimum for a coupled bump); every chart has an upgrade recipe; every release is deployed; a
coupled hop is for the cluster's current or next minor. A bump that is not an upgrade, and a refresh that
names a Kubernetes minor, are refused as bad input.

**Done** when every chart runs its target under its current identity, with a deployed release.

**Recipes:** each chart has its values and a check beyond Helm's `--wait`.

| Chart | Values | Extra check |
|---|---|---|
| metallb | ours | Helm `--wait` |
| metrics-server | ours | the `v1beta1.metrics.k8s.io` APIService is Available |
| prometheus-operator-crds | none | every CRD of the release is Established |
| external-secrets | ours; the release is found in any namespace | Helm `--wait` |
| node-exporter | ours | Helm `--wait` |
| alloy | the release's own values, plus what the target version needs | Helm `--wait` |

Alloy keeps its values because they come from `alloy cluster install` inputs (remotes, cluster name,
profile) that are not saved anywhere.

**Steps:** check that metrics-server's target suits the running Kubernetes minor. Then CRD charts first,
the rest in plan order. For each chart: refuse a release under another chart; download and verify;
`helm upgrade --wait`; wait until the release is deployed at the target and its extra check passes. A chart
already at its target is skipped.

**On failure:** the first failure stops the hop, with a `helm rollback` hint; charts already moved stay. A
release left failed blocks the next plan (`release-not-deployed`) until it is rolled back or finished.

## The command

`kube cluster upgrade` is experimental: it runs only with `--experimental` or
`SOLO_PROVISIONER_ENABLE_CLUSTER_UPGRADE=1`.

| Mode | Lock | Changes | Exit 0 when |
|---|---|---|---|
| `--plan` | none | nothing | the plan has no blockers |
| `--dry-run` | none | nothing | no blocker and every pre-hop check of the next hop passes |
| (default) | `system`, `cluster` | the next hop | the hop worked, or the cluster is at the target |
| `--all` | `system`, `cluster` | every remaining hop | every hop worked |

- **The lock** is taken before the first read, with no wait, and held until the run ends. Any other command
  that changes the cluster is refused with the holder's pid. The confirmation runs under the lock, so the
  hops confirmed are the hops that run.
- **`--all`** plans again from the live cluster after each hop. It stops at the first failure, when the
  next plan cannot be made (a re-run resumes), and when the plan changes from the hops the operator
  confirmed. Two internal guards stop a loop: a plan that starts with the hop just applied, and more hops
  than the first plan's length plus two.
- **At the target**, the run checks the node, waits until the cluster can be read, and records the cluster
  and the hashed host binaries. It appends an entry (`component: cluster`, `step: record`) only when a
  record changed, the write failed or the workflow failed, so a hop whose record failed is recorded by the
  next run, and a run with nothing to fix adds nothing. A walk that reaches the target does the same.
- **Error modes:** only `--stop-on-error`. `--rollback-on-error` and `--continue-on-error` are refused.
- **Output:** each hop's workflow, then the result: the hops applied, the outcome (`applied`, `walked`,
  `at-target`, `blocked`, `declined`, `stopped`, `failed`), why the run stopped, and the next hop. With
  `--output json` the last object has `"type": "upgrade"`. On a terminal, `--output json` needs `--force`,
  since the question would mix into the JSON; a run without a terminal applies without asking.

## Tests

- Unit: `go test ./internal/bll/clusterupgrade/ ./internal/workflows/steps/ ./cmd/cli/commands/kube/cluster/`.
- On a VM, as root, against the live cluster (`require_cluster` tag): `TestWithCluster_ApplyNextHop` runs
  the engine (one hop, or every hop with `HOP_ALL=1`); `TestWithCluster_ApplyCiliumHop`,
  `TestWithCluster_ApplyKubernetesHop` and `TestWithCluster_ApplyStructuralHop` run one executor.
