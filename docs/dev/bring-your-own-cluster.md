# Bring Your Own Cluster and Per-Cluster State

Status: **Draft for review.** Nothing in this document is implemented yet.

> **IMPORTANT: external-cluster support is a provisioner-test-only feature.**
> Pointing weaver at a cluster it did not provision (`--kubeconfig`, "external
> mode") exists so we can test `solo-provisioner` itself against a throwaway
> cluster such as Kind, without a KVM or a privileged runner. It is **not** a
> supported way to run block node, consensus node or any other component in
> production, and we do not encourage operators to use it in their own CI or
> against their own clusters. It is gated behind the experimental feature flag
> (`--experimental` / `SOLO_PROVISIONER_ENABLE_EXTERNAL_CLUSTER`) and refuses to
> run without it. The provisioned, single-node path on a Linux host stays the
> only supported way to operate a real deployment.

Depends on the per-component state files work: #1231 (PR #1235) and #1245. This design is written in terms of those component files and should be built after they merge. See section 6.

## 1. Problem

`solo-provisioner` assumes one machine runs exactly one cluster, and that the cluster is the one it installed. The code does not check this.

- `kube.ProbeCluster()` (`internal/kube/client.go`) only asks if a kubeconfig is reachable. It reads `$KUBECONFIG` or `~/.kube/config`, stats the file, and calls `/version` with a 3 second timeout.
- `clusterChecker.RefreshState` then sets `ClusterState.Created = true` for any reachable cluster.
- `bll/blocknode` and `bll/consensus` install handlers branch on `Created`. A block node install can run against whatever cluster the operator's shell points at, and still run host-level steps (firewall, tc shaping, bind mounts, weaver user).
- Kubeconfig resolution is duplicated in `loadKubeConfig`, `resolveKubeconfigPath`, `step_daemon_provision.newTypedClient`, Helm's own settings, the daemon (`kubeconfigPath` in six places), and two hard-coded `/etc/kubernetes/admin.conf` constants.
- State has no notion of which cluster it describes. Today it is one file, `/opt/solo/weaver/state/state.yaml` (being split into per-component files by #1231), so a second cluster would overwrite the first one's state.

We also want to run most UAT against a cluster we did not install (for example Kind), so CI does not need KVM or privileged runners for every test.

## 2. Goals and non-goals

Goals:

1. Never trust a cluster as ours unless we can prove we installed it on this machine. Check this as early as possible, and stop if the proof fails.
2. For testing `solo-provisioner` itself only, and behind the experimental gate, let the CLI point at a cluster it did not provision (`--kubeconfig`) and do API-level work there: install, upgrade and uninstall of block node, consensus node and the other cluster components. This is not for production use (see the banner above).
3. Keep state separate per cluster, so switching between clusters, or forgetting a flag, can never mix or reset state.
4. One place decides which kubeconfig is used.
5. Leave room to manage several VMs from macOS later.
6. Stop two operators (or an operator and the daemon) from changing the same component of the same cluster at the same time.

Non-goals for now:

- SSH delegation to remote hosts (`--host`). See section 9.
- Storing state inside the cluster.
- A full darwin build of the CLI.
- Using the cluster-side lock (section 5.5) as a state store. It only says who is changing a component right now.
- Locking the first `solo-provisioner kube cluster install`. No cluster exists yet, so only the local `flock` applies.
- Taking over a live lease automatically. Only expiry releases it, and an explicit operator override removes a dead one.
- Protecting against changes made outside weaver (`kubectl`, Helm, other tools). The lease only coordinates weaver processes, the CLI and the daemon.

## 3. Concepts

| Term | Meaning |
|---|---|
| Host | The machine the binary runs on. Today it is always `local`. |
| Cluster ID | The UID of the `kube-system` namespace. Stable for the life of a cluster, unique, and independent of IP or context name. |
| Origin `provisioned` | `solo-provisioner` installed this cluster (kubeadm, CRI-O, kubelet) on a host. |
| Origin `external` | Someone else's cluster. We only talk to its API. |

The mode is **derived, not declared**. The `--kubeconfig` flag only chooses which cluster to look at. The provenance check (section 5) decides whether it is `provisioned` on this host or `external`. This is what stops flag flip-flopping from corrupting state.

## 4. Cluster resolution

Add one resolver, `kube.ResolveCluster(opts)`. It returns the REST config, the kubeconfig path, the context, the cluster ID and the origin. Every caller listed in section 1 uses it.

New root persistent flags:

| Flag | Meaning |
|---|---|
| `--kubeconfig <path>` | Kubeconfig to use. Without it, only the cluster this host provisioned is considered. |
| `--context <name>` | Context inside the kubeconfig. |
| `--cluster-name <alias>` | Optional human label, stored in `clusters/index.yaml` when the cluster is registered (section 4.1). Defaults to the context name. Never used as a key. |

Without `--kubeconfig`, ambient `$KUBECONFIG` and `~/.kube/config` are not trusted on their own. Weaver already copies `admin.conf` to `~/.kube/config` for root and the sudo user (`internal/kube/admin.go`), so that file can be ours, but the provenance check, not the path, proves it.

### 4.1 Registering a cluster

A cluster is registered once, by one command, and the rest of the commands refer to it. `connect` is part of the experimental, provisioner-test-only external-cluster feature (see the banner at the top), so it is hidden from `--help` and refuses to run without `--experimental` or `SOLO_PROVISIONER_ENABLE_EXTERNAL_CLUSTER`:

```bash
solo-provisioner kube cluster connect --experimental --kubeconfig <path> [--context <name>] [--cluster-name <alias>]
```

`connect` does these steps, in order:

1. Load the kubeconfig and test the connection with a short deadline. If it fails, stop with a clear error.
2. Read the cluster ID (the `kube-system` UID).
3. Classify the cluster with the evidence and outcomes in sections 5.1 and 5.2: provisioned on this host, or external.
4. Check the permissions it can check at this point, with `SelfSubjectAccessReview`: API access, and creating namespaces and leases. Checks in a component's own namespace run later, when a command needs them, because that namespace may not exist yet.
5. Bind the alias to the cluster ID. If the alias is already bound to a different cluster ID, stop unless `--rebind` is given.
6. Write the `clusters/index.yaml` entry. State files are not created here. They are created when a component is first managed in that cluster.

Related commands, all reading or editing the index only:

| Command | Purpose |
|---|---|
| `solo-provisioner kube cluster list` | Print every known cluster: alias, short ID, origin, server, last used. |
| `solo-provisioner kube cluster info <alias>` | Origin, server, the components managed there (block node, consensus node and so on) and their state, and the current lease holders (section 5.5). Runs the provenance check in report-only mode, so it pairs with `doctor` and `diagnose` (#1110). |
| `solo-provisioner kube cluster disconnect <alias>` | Remove the index entry. State files are left in place until an explicit prune (section 12). Later work. |

A cluster that passes the provisioned proof (section 5.1) registers automatically: `solo-provisioner kube cluster install` does it for new installs, and the gate does it on its first successful pass for older installs (section 5.4). No `connect` is needed. The rule below applies to every other cluster, which is external.

A command that is given an unregistered external cluster:

- Interactive: stops with a hint to run `solo-provisioner kube cluster connect`.
- Non-interactive (`--non-interactive`, CI): registers the cluster implicitly with the same steps, so a Kind lane needs no extra step.

`connect` is idempotent. Running it again for the same cluster ID refreshes the context, server and last-used time.

## 5. Provenance gate

A check in the root pre-run, **before** any RSL or reality check. It decides which cluster a command is about and whether this host provisioned it. If the answer is unsafe, the command stops with a reason code and a hint. This is how we know reality and state can be trusted.

The gate prevents accidents: running against the wrong cluster, or against state left behind by another one. It is not a security boundary. A user with root can fake any of its evidence.

The gate reads only `hosts/local/host.yaml` and the software records, read-only and without locks. It does not go through RSL, reality or the state manager, but it reads these files through #1235's component loader (`ComponentFilePath`), not a second parser, so the on-disk format has one reader. Atomic writes (#1235 replaces each file by rename) make a lock-free read safe.

**Terms.** "Identity" is overloaded, so this design uses one noun per thing and never the bare word:

| Term | Meaning |
|---|---|
| **HostIdentity** | the machine — `/etc/machine-id` |
| **ClusterIdentity** | the cluster — the `kube-system` UID (plus server address and CA fingerprint) |
| **Provision record** | what this host recorded about the cluster it provisioned — that cluster's `ClusterIdentity`, bound to this `HostIdentity`, in `hosts/local/host.yaml` |
| **Target** | the cluster a command is pointed at (via `--kubeconfig`, or the host's own cluster by default) |
| **Origin** | the verdict: `provisioned` or `external`. A refusal is not an origin — it is a stop |

The gate resolves the **Target**, compares its **ClusterIdentity** against the **provision record** for this **HostIdentity**, and returns an **Origin** (or stops).

### 5.1 Evidence

The gate separates evidence that needs no API from evidence that needs it, so a broken local cluster can still be recognised as ours.

**Local evidence** (no API needed). All of these must pass for the host to count as running a provisioned cluster:

| Evidence | Why |
|---|---|
| Software state records kubelet and CRI-O as installed, and the sandbox binaries on disk match the catalog checksum for the recorded version and platform | Binary provenance |
| kubelet and CRI-O are running (systemd unit active, CRI-O socket answers), and the running executable is the sandbox binary, not one from `PATH` | A matching file is not enough |
| `/etc/kubernetes/admin.conf`, the static pod manifests under `/etc/kubernetes/manifests` and `/etc/kubernetes/pki/ca.crt` exist | kubeadm control plane evidence |
| The server host in `admin.conf` resolves to an address of this machine | The cluster is on this host |

The address check alone is not enough. A Kind or minikube cluster on the same machine points at `127.0.0.1` and would pass it. The binary and process checks rule those out.

**Provision record** (also no API needed). `hosts/local/host.yaml` records, after the first successful pass, the cluster ID, the server address and the CA certificate fingerprint of the cluster this host provisioned. A kubeconfig **matches the provision record** when its CA fingerprint equals the recorded one. The server address is a fallback, used only when no fingerprint is recorded, which can happen on an older install. The fingerprint is the real cluster identity. A server address can be reused: a different cluster rebuilt on the same `IP:6443` would match on the address alone, so weaver would route it through the "this is our cluster" branch (branch A) and report it as "rebuilt, state stale" (rule A.4) instead of as external (rule C.1). The live cluster-ID check still stops any state corruption in that case. Preferring the fingerprint only makes the message the operator sees correct. This works while the cluster is down, which the cluster ID alone cannot.

**Live cluster identity** (needs the API). The `ClusterIdentity`, the UID of the `kube-system` namespace, read from the live cluster.

Reading evidence can fail for a reason other than a mismatch. For example, checking the running executable (`/proc/<pid>/exe`) generally needs root. The gate keeps "cannot read" apart from "does not match": an unreadable check is never taken as proof that a cluster is not ours.

Assumption, to be confirmed against the kubeadm workflow: a host runs one single-node cluster, so the control plane is local.

### 5.2 Outcomes

Only the needs that reach the live cluster, `api` and `host+api` (section 5.3), evaluate these rules. The `host`, `install` and `connect` needs run a reduced check that never reads the API, so the "API unreachable" leaves do not apply to them. Section 5.3 says exactly what each need checks.

The outcomes fall into three branches: the target matches the provision record, nothing is recorded yet, or the target is not this host's cluster. One precondition is checked before all of them. Within a branch the rows are checked in order and the first match wins. The `#` column (for example A.1) is the stable rule id used elsewhere in this document.

**Precondition (all branches):**

| # | Condition | Result |
|---|---|---|
| P.1 | The recorded `machineId` in `host.yaml` does not match this machine (for example the directory was copied) | Stop. Hint: the state belongs to another machine |

**Branch A — the target matches the provision record** (CA fingerprint; server address only as a fallback). Reads the API.

| # | Condition | Result |
|---|---|---|
| A.1 | A local check fails, or the API is unreachable | Stop. The hint names the failing piece (for example kubelet is installed but not running). **Never** fall back to `external` |
| A.2 | A local check could not be read (for example no root) | Stop. Hint: run with `sudo` |
| A.3 | Local evidence passes, the API is reachable, and the live cluster ID matches the recorded one | `provisioned`, host `local`, state in `hosts/local` |
| A.4 | Local evidence passes, the API is reachable, but the live cluster ID differs | Stop. The cluster was rebuilt and the state is stale (decision 2 in section 12) |

**Branch B — nothing is recorded, but local evidence passes** (an older install, section 5.4; trust on first use). Reads the API.

| # | Condition | Result |
|---|---|---|
| B.1 | The API is unreachable | Stop. The cluster ID cannot be read. Hint: start the cluster, or run `solo-provisioner kube cluster connect` once it is up |
| B.2 | The live cluster ID already has `clusters/<uid>` state | Stop. Needs an explicit step; it is never adopted silently (see below) |
| B.3 | The live cluster ID has no `clusters/<uid>` state | `provisioned`. Write the provision record |

**Branch C — the target is not this host's cluster** (a provision record exists but the target does not match it, or nothing is recorded and local evidence does not pass). Decided from `--kubeconfig`; no API needed.

| # | Condition | Result |
|---|---|---|
| C.1 | `--kubeconfig` was given | `external`, state in `clusters/<uid>`. The cluster must be registered (section 4.1): an interactive run stops with a hint to run `solo-provisioner kube cluster connect`, a non-interactive run registers it first |
| C.2 | No `--kubeconfig` was given | Stop. If nothing is installed here, the hint is to run `solo-provisioner kube cluster install` or pass `--kubeconfig` for an external cluster. If a provisioned install is only partly present, the hint names what is missing |

Branches A and C together prevent split-brain between the state locations: a cluster that does not match the provision record can only become `external`, never overwrite `hosts/local`. Rules A.4 and B.2 cover the two ways one cluster ID could end up described twice. Rule A.1 is the one that keeps a broken local cluster from being treated as someone else's.

#### Rule B.2: a cluster that was external and now passes the local proof

This can happen when a cluster on the machine was not recognised at first (for example kubelet was down, or it was built by hand), so a command with `--kubeconfig` treated it as `external` and created `clusters/<uid>/`. Later the proof passes and the same cluster ID now qualifies as `provisioned`. Two places could then describe one cluster, and the records may disagree. Picking one silently could ignore the block node and consensus information in the other, and merging automatically could overwrite newer facts with older ones. So weaver stops and asks.

The explicit step is one of:

1. **Discard the external copy.** The operator archives or removes `clusters/<uid>/` and reruns. External state is a cache (decision 4), so `hosts/local` is rebuilt from what the cluster reports.
2. **Adopt it.** A command copies the cluster-scoped component files (`cluster`, `blocknode`, `consensus` and the cluster half of `teleport`) from `clusters/<uid>/` into `hosts/local/`, then archives the external directory.

Until a command for this exists (section 13), the error lists the manual steps.

### 5.3 What each command needs

A fixed exempt list does not fit, so each command declares what it needs, and the gate checks only that:

| Need | Meaning | Commands | What the gate does |
|---|---|---|---|
| `none` | No cluster and no host evidence | `solo-provisioner install`, `solo-provisioner uninstall`, `solo-provisioner version`, `solo-provisioner demo`, `--help`, `solo-provisioner kube cluster list` | Nothing |
| `connect` | Classifies the target itself | `solo-provisioner kube cluster connect` | Runs the evidence and outcomes in 5.1 and 5.2 to classify the target, and registers it instead of stopping |
| `install` | Creates the cluster | `solo-provisioner kube cluster install` | Allows no cluster, or stale state (rule A.4, which this command resolves). Refuses when a cluster that is not ours is already on this host, which means `/etc/kubernetes/admin.conf` exists but the evidence does not show it is ours |
| `host` | Host-level work that does not need the API, and must also work on a broken cluster | `solo-provisioner kube cluster uninstall`, `solo-provisioner network firewall\|shape\|policy ...`, `solo-provisioner teleport node ...` | Offline check only: the machine ID matches (rule P.1), and the run is not in external mode (no `--kubeconfig` points elsewhere). It never reads the API, so a down or unhealthy local cluster does not stop it. Branches A, B and C are not evaluated |
| `api` | Work through the Kubernetes API in either mode | Block node, consensus node, alloy, ESO and teleport cluster commands that do not touch the host | The full outcomes in 5.2. The result may be `provisioned` or `external` |
| `host+api` | Provisioned-only work that also needs the cluster | Commands that run host steps and API steps, such as block node install on a provisioned host | The full outcomes in 5.2, and the result must be `provisioned` |

Diagnostic commands (`doctor`, `diagnose` and `solo-provisioner kube cluster info`, epic #1110) need `api`, but run the gate in **report-only mode**. A failed outcome is printed as a finding and does not stop the command. The detail of those commands belongs to #1110.

The exact assignment of each command is made when the gate is built, from the command tree under `cmd/cli/commands`.

### 5.4 Clusters installed by older versions

A strict gate must not lock operators out of clusters that older versions installed. Two things could be missing on such a machine, and neither needs a separate migration:

- **Recorded hashes.** Software state records only the name, version, `Installed` and `Configured`; it has never held a binary hash. The gate does not need one. The embedded catalog has the expected checksum for each software, version and platform, which the installer already checks at download. The gate verifies that the sandbox binary on disk matches the catalog checksum for the version recorded in state, and that the running process is that binary.
- **The provision record.** `hosts/local/host.yaml` has no cluster ID, server address or CA fingerprint on an older install. The first time all the local evidence passes and the live cluster ID can be read (rule B.3), the gate records all three. This is trust on first use, and it is safe because the binary, process, `admin.conf` and address checks have already passed. Running `solo-provisioner kube cluster connect` is an optional way to register eagerly and see each check reported (section 4.1); nothing stops working if it is skipped.

Catalog gaps: the catalog is refreshed and split over time (#1262), so an old kubelet or CRI-O version may no longer be in it. If the recorded version is not in the catalog, the checksum check is skipped with a warning, and the gate relies on the other checks: the binary is in the sandbox path and the running process is that binary. A version that is in the catalog but does not match its checksum still stops the command.

The acceptance test is the existing `uat:compat` scenario, which installs a released version and upgrades. After the upgrade the gate must pass with no manual step.

### 5.5 Cluster-side lock

The per-component `flock` files (#1235) only protect one machine. Two operators on different machines (for example a laptop in external mode and a CLI on the VM) can still change the same cluster at once. A Lease object in the cluster closes that gap. The idea follows the lease in `hiero-ledger/solo` (`src/core/lock/`), with a different renewal and loss model.

Decisions:

- **Object.** A `coordination.k8s.io/v1` Lease, one per component per cluster, named `weaver-<component>` (for example `weaver-blocknode`), in the **component's own namespace**. Per-component leases mirror the per-component state locks, so disjoint components (for example consensus and teleport) do not block each other. The lease disappears with the namespace on uninstall.
- **Both modes.** Every mutating command takes the lease as soon as the cluster is reachable, whether the cluster is `provisioned` or `external`. Read-only commands do not. Order: cluster lease first, then the local component `flock`.
- **Library.** `k8s.io/client-go/tools/leaderelection`, wrapped in a small internal package, `internal/kube/lease`. It handles the compare-and-swap on `resourceVersion`, renewal, and loss detection. Adding it needs `go mod vendor`, as for any dependency.
- **Wrapper API.** `TryAcquire(ctx, component)` returns a handle with a context that is cancelled when the lease is lost, and `Release()`.
- **Fail fast.** Before starting the elector, read the Lease. If it is held and not expired, stop and print the holder: user, host (machine ID), command line, start time and weaver version, taken from the holder identity (JSON), and how long until it expires. If the elector loses a race, a short acquire timeout turns that into the same error.
- **Fencing.** If renewal fails past the renew deadline, the wrapper cancels the workflow's context and the command aborts. A laptop that sleeps past expiry can therefore not resume and keep changing a cluster another operator now holds. This only stops a mutation if the step in flight honours context cancellation, so the mutating steps must return promptly on `ctx.Done()` rather than run a long host or API call to completion. Auditing the mutating steps for this is part of delivery step 2.
- **Expiry only.** The lease is taken over only after it expires, because a live holder keeps renewing. There is no other automatic takeover. An explicit override flag (name to be decided) lets an operator remove a lease they know is dead.
- **Timings.** Starting values: lease duration 30 seconds, renew deadline 20 seconds, retry period 5 seconds. To be tuned against slow API servers, where a renewal that is merely slow, not lost, could fence a legitimate long operation; the renew deadline must stay comfortably above the worst-case round trip to the API.
- **First install.** The cluster or the component namespace may not exist yet. Cluster install is covered by the local `flock` only. For a component namespace that does not exist, create the namespace first (an `AlreadyExists` error is fine) and then take the lease. The race window is only the namespace creation.
- **RBAC.** External operators need `get`, `create`, `update` and `delete` on `leases` in the component namespace. The preflight checks this with `SelfSubjectAccessReview` and fails with a clear hint.
- **Daemon.** The daemon takes the same lease when it writes consensus state, so it excludes CLI runs.
- **Diagnostics.** `doctor` and `diagnose` show current lease holders as a finding.

## 6. State layout

### 6.0 What it builds on

State is moving from one `state.yaml` to per-component files, each with its own hash and lock:

- #1231 / PR #1235: `machine.yaml`, `cluster.yaml`, `blocknode.yaml`, `consensus.yaml`, `teleport.yaml`, with a startup migration.
- #1245: `machine.yaml` split further into `profile`, `firewall` and `system`.

We build on that. The per-component files are what make the host and cluster split structural. One change is needed in #1245: `ProvisionerState` and `Version` (the provisioner's own version, commit and executable, and the state model version) move out of `system` into a binary-level file, `provisioner.yaml`, described below. That is cheaper to agree now, while #1245 is open.

### 6.1 Layout

State is grouped by host. A cluster with no host on this machine gets its own directory holding only the cluster-scoped component files. The tree below shows the layout once #1235 and #1245 have both merged, plus the binary-level file. Until #1245 lands, `profile`, `firewall` and `system` are still one `machine.yaml`.

```
/opt/solo/weaver/state/
├── provisioner.yaml               # binary-level: cli and daemon sections, each with version, commit, executable, state model version
├── hosts/
│   └── local/                     # this machine + the cluster it provisioned
│       ├── host.yaml              # host identity + provision record: name, machine ID, the cluster this host provisioned
│       ├── profile.yaml           # host components
│       ├── firewall.yaml
│       ├── system.yaml
│       ├── cluster.yaml
│       ├── blocknode.yaml
│       ├── consensus.yaml
│       └── teleport.yaml
└── clusters/
    ├── <kube-system-uid>/         # one per cluster with no host here
    │   ├── cluster.yaml           # cluster-scoped components only
    │   ├── blocknode.yaml
    │   ├── consensus.yaml
    │   └── teleport.yaml
    └── index.yaml                 # complete view of every known cluster
```

Rules:

- `provisioner.yaml` describes the binaries, not a host or a cluster. It is always loadable, the binary reads it first before choosing a host or a cluster, and it exists on every machine, including a Mac with only external clusters (`~/.solo-weaver/state/provisioner.yaml`). Startup migrations depend on the recorded provisioner version, and without this file a machine with no `hosts/local` would have nowhere to record it. Host and cluster state never write it. The file holds one section per binary, `cli` and `daemon`, each with that binary's version, commit, executable path and state-model version. The CLI and the daemon ship and upgrade on their own schedules, and the daemon can self-upgrade (epic #500) to a version the CLI never installed, so each binary is the authority on its own version: each writes only its own section and reads the other's. This gives both binaries a reliable view of each other's version and existence, so either can warn on a skew. Because two binaries write one file, a write is a read-modify-write under the file's lock (#1235), not a blind atomic replace, so neither section clobbers the other; reads stay lock-free. The daemon section is empty until a daemon is installed. Startup migrations are a CLI concern and target the version in the `cli` section.
- `hosts/local/host.yaml` is a small host-identity file that the provenance gate reads first, before any component file, so it needs no component lock. It holds the host `name`, `kind` (`local`; `remote` later), the `machineId` from `/etc/machine-id`, and the provision record of the cluster this host provisioned (empty when none): `managedClusterId`, `serverAddress` and `caFingerprint`, which the gate uses to recognise it even when its API is down (section 5.1). A later remote host (section 9) uses the same file with extra registry fields. If the recorded `machineId` no longer matches the machine, for example because the directory was copied, the command stops. If the machine has no usable `/etc/machine-id` (some containers), the check is skipped and no ID is recorded.
- `hosts/local/` otherwise keeps today's component files and content.
- `clusters/<uid>/` holds **only cluster-scoped components**. Host components (`profile`, `firewall`, `system`) never exist there, so external-mode code cannot reach host state. Hashes and locks work per file, as in #1235.
- Each file records its own `clusterId` and `origin`, and the loader checks them against the live cluster. A copied file is rejected.
- External runs never write `hosts/local`. Provisioned runs never write `clusters/<uid>`.
- The key is the cluster ID, not the IP, the context name or a user-supplied name. A rebuilt cluster gets a new ID and therefore fresh state.

### 6.2 Host-bound fields inside component files

`clusters/<uid>/` holds `blocknode.yaml`, `consensus.yaml` and `teleport.yaml` for the components managed in that cluster, with the same names and schema as in `hosts/local/`. A few fields inside those files only make sense on a provisioned host:

| File | Host-bound fields | Cluster-level fields |
|---|---|---|
| `blocknode.yaml` | `Shaping`, `TrafficShapingDisabled`, `Storage` (hostPath) | Helm release info, plugin preset and list, retention |
| `consensus.yaml` | `HostPathOwners` | Node identity, images, config hashes, secrets, managed spec |
| `teleport.yaml` | Node agent | Cluster agent |

In external mode the steps that fill the host-bound fields (tc shaping, hostPath storage, the node agent) do not run, so those fields stay empty. The question is how to make sure they can never be filled in a cluster directory. Two ways:

1. **Optional fields plus a writer guard (the default).** The state writer refuses to write a non-empty host-bound field to any file under `clusters/<uid>/`, and a unit test covers every field in the table. It keeps the schemas as they are.
2. **Split the files by owner**, as #1245 does for `machine.yaml`, for example a host-side file for shaping and storage that exists only in `hosts/local/`. The compiler then enforces it. It is more work and touches the daemon's `consensus.yaml`, so keep it as later hardening if the guard proves leaky.

The deployment profile (`local`, `testnet`, `mainnet`) belongs to its target, because one person can manage a `local` Kind cluster, a `perfnet` cluster and a `mainnet` VM. For a provisioned host it is `hosts/local/profile.yaml`, as in #1245. For an external cluster it is a `profile` field in `clusters/<uid>/cluster.yaml`. A default for new targets still comes from the config file or `--profile`; that is configuration, not state.

The daemon writes `consensus.yaml` and is released separately, so it has to follow any path or layout change.

### 6.3 `clusters/index.yaml`

The index lists **every** cluster, including the provisioned one, so it is the complete view. A cluster with `state: hosts/local` has no `clusters/<uid>/` directory.

```yaml
clusters:
  - uid: 3f2a91bc-...
    alias: local
    origin: provisioned
    host: local                    # state: hosts/local
    server: https://10.20.0.5:6443
    lastUsed: 2026-10-15T09:12:00Z
  - uid: 7c1e9a04-...
    alias: perfnet-eu
    origin: external               # no host field
    state: clusters/7c1e9a04-...
    context: perfnet-eu-admin
    server: https://k8s.perfnet-eu.example:6443
    lastUsed: 2026-10-15T09:40:00Z
```

#### How a cluster is identified to a human

The cluster ID is the UID of the `kube-system` namespace, a random UUID that says nothing about the cluster. It stays the key because it is stable and unique. Everything a person reads shows more than the bare ID:

- **Alias.** From `--cluster-name`, or the kubeconfig context name when none is given (for example `kind-dev`). A label only.
- **Short ID.** The first 8 characters of the UID.
- **Server address.** For example `https://10.20.0.5:6443`.

Every message, log line and diagnostic shows them together, for example `perfnet-eu (7c1e9a04) at https://k8s.perfnet-eu.example:6443`. On disk, `clusters/<uid>/` stays a plain UID directory, because putting the alias in the name would mean renaming it when the alias changes. The index maps one to the other, and a read-only list command prints it (section 13).

The alias binds to a cluster ID when the cluster is registered with `solo-provisioner kube cluster connect` (section 4.1). If the alias later points at a different cluster ID, the command stops with an error. `--rebind` overrides it explicitly. An alias alone can never create state.

## 7. External mode: API only

> In external mode weaver only talks to the Kubernetes API. It touches nothing on the host.

External mode is experimental and provisioner-test-only (see the banner at the top). It is reached only when the experimental gate is on, through the hidden `--experimental` flag or `SOLO_PROVISIONER_ENABLE_EXTERNAL_CLUSTER=1`, reusing `common.GateExperimental` (`cmd/cli/commands/common/experimental_gate.go`). Without the gate, `--kubeconfig` pointing at a cluster that is not this host's own is refused with a reason code and a hint, and `solo-provisioner kube cluster connect` is hidden from `--help`.

- Skipped: node setup, kubeadm setup, network setup (nft, tc), bind mounts, sysctl, swap, weaver user, daemon config.
- Storage: hostPath PVs refer to a node filesystem weaver cannot own, so they are not used. Storage comes from the cluster through a `storageClassName` per volume (flag or values file). Weaver creates PVCs only, never PVs. The hostPath templates (`storage-config.yaml`, `optional-storage.yaml`) are provisioned-only.
- Preflight checks API-level facts only: server version range, access via `SelfSubjectAccessReview`, and that the CNI and StorageClass exist.
- Provisioned-only commands fail in external mode with a reason code and hint: `solo-provisioner kube cluster install`, `solo-provisioner kube cluster uninstall`, `solo-provisioner teleport node ...`, and `solo-provisioner network firewall|shape|policy ...`.
- Uninstall and reset remove only what weaver created in that cluster, tracked by labels or state. Cluster-level components are never touched.
- This also applies on a provisioned host: pointing it at a remote cluster with `--kubeconfig` runs external mode and skips the host steps.

### 7.1 Local development with Kind

To test `solo-provisioner` itself, a developer can create a Kind cluster on their own machine (including a Mac) and use it with weaver, behind the experimental gate. It does not need SSH delegation (section 9). This is for exercising weaver against a throwaway cluster, not for running a real deployment on Kind.

- Weaver did not install it, so it is `origin: external`. It gets a `clusters/<kube-system-uid>/` directory and an index entry with the context (for example `kind-dev`) and a `127.0.0.1` server address.
- Register it once with `solo-provisioner kube cluster connect`, or let a non-interactive run register it. Use it with `--kubeconfig` and `--context`. Only API-level work runs. Storage comes from Kind's default StorageClass.
- Each `kind create cluster` gets a new cluster ID, so a recreated cluster starts with clean state. If the same alias is reused for the new cluster, `--rebind` is needed.
- The same flow is what the CI Kind lane uses (section 10).

Prerequisites for running this on macOS:

- The CLI must run on darwin. Build stubs for the Linux-only code (`mount`, `modprobe`, swap) exist, but running the CLI on macOS has not been verified.
- The state directory defaults to `~/.solo-weaver/state` on macOS, because `/opt/solo/weaver` is a Linux path.

## 8. Migration of existing deployments

Add a startup migration, `state-hosts-layout`, registered in `RegisterMigrations()` (`cmd/cli/commands/root.go`) **after** the migration that splits `state.yaml` into component files (#1231). See `docs/dev/migration-framework.md`. The chain is: `unified-state`, then per-component split, then `state-hosts-layout`. Each step is idempotent.

- `Applies`: component files exist directly under `state/` and `state/hosts/local/` does not.
- `Execute`: create `hosts/local/` with the same owner and mode, move the component files (and their lock and hash companions) into it, and keep a backup of the originals as `state/.pre-hosts-layout/`. It moves files and does not rewrite their content, with one exception: it copies `ProvisionerState` and `Version` out of the machine-level file into the `cli` section of `state/provisioner.yaml` (the `daemon` section stays empty until a daemon writes it, section 6.1), and writes `hosts/local/host.yaml` (name, kind, machine ID, and the cluster ID if one is already recorded).
- If both locations have files, prefer `hosts/local/`, leave the legacy files untouched, and log a warning. Never overwrite.
- Idempotent and crash-safe. `Rollback` restores the backup on a best-effort basis.
- It runs before anything opens the state manager, and before the provenance gate. Migration is pure filesystem work and needs no cluster.
- All code that builds a state path (the state manager, `state_reader.go`, the existing state migrations, `testutil`, the daemon) moves to one resolver. #1235 already builds component paths from a directory (`ComponentFilePath(dir, id)`, `RuntimeResolver.StateDir()`), so for most callers the change is what that directory is (`state/` becomes `state/hosts/local/`). A few callers still use `models.Paths().StateDir` directly, such as the startup-migrations lock and some tests.
- The migration runs under the same startup-migrations lock that #1235 takes on the state directory.
- The split migration from #1235 can leave `state.yaml.legacy` and `state.yaml.diverged-*` files in `state/`. They stay where they are: the layout migration moves only the component files, and does not touch or delete them.

Risks to settle:

1. Any path stored inside a state file (the old `StateFile` field is the known case) must be rewritten, and the loader should use the path it actually loaded from, or a later flush could write to the old location.
2. Downgrade: an older binary looks at the old path and sees a fresh machine. Migrations are one-way today; document it. A symlink does not help, because atomic writes replace it.
3. The daemon is a separate binary with its own release schedule. It must read the new location, and a daemon older than the CLI must not see an empty state.
4. Two concurrent invocations during the move need a lock, or an atomic rename plus a re-check. The per-component `flock` files from #1235 move together with the state files, so the move must hold all of them.

## 9. Later: several VMs from macOS

Not in scope now, but the layout leaves room.

- A developer on macOS runs a launcher that copies the matching Linux binary to a VM, verifies its digest, and runs the command there over SSH. The VM keeps its own `hosts/local` state.
- The Mac keeps `hosts/<name>/host.yaml`, the same file `hosts/local` uses, with `kind: remote` and registry fields added: SSH target, verified machine ID, last known binary version. It never holds a copy of a VM's state.
- The Mac can still drive that VM's cluster natively through the API under `clusters/<uid>/`, and delegate host-level commands with `--host <name>`.
- In the index, such a cluster is `origin: provisioned` with `host: <name>`.
- Open design points: version skew, binary provenance (signatures and digests), flag-to-file path rewriting, secrets in uploaded files, PTY and non-interactive mode, SSH host-key trust, and keeping a command running after a dropped connection.

## 10. Effect on CI

This section is about `solo-provisioner`'s own CI, which is the one place external mode is meant to run. It is not a recommendation for operators' pipelines. External mode is what makes a Kind lane possible on the ARC runners (`solo-linux-medium`) without privileged pods. Tests then split into:

| Lane | Runner | Content |
|---|---|---|
| Unprivileged | `solo-linux-medium` | Unit tests, installer download and verify tests |
| Kind (external mode) | `solo-linux-medium` with `dind` | API-level UAT: chart and plugin-preset upgrade, kube client, alloy, teleport cluster, solo-operator |
| Host-level (provisioned) | Hosted runner or privileged systemd container | Network, mount, sysctl, systemd tests and the full cluster install UAT |
| Real VM | `kvm-runner`, nightly, started by a path filter, or enabled by hand | Boot-order and reboot tests, one full Debian lifecycle |

### 10.1 Rollout order and baseline

What this design gives CI:

- Most PR jobs stop booting a KVM VM. Today the PR gate boots five (four UAT jobs and the integration job), and three of the UAT jobs repeat the full cluster install.
- Jobs become isolated and parallel. A Kind cluster per job replaces the shared `/opt/solo` and one serial integration run. Where several jobs do share a cluster, the lease (section 5.5) keeps them from interfering.
- Most tests no longer need privileged runners.
- External mode is tested on every PR. One useful check comes with it: a Kind cluster on the same machine must be classified as `external` and refused as `provisioned`, which covers the IP-only trap in section 5.1.

What it does not do:

- Host-level tests (nft, tc, sysctl, swap, mounts, systemd, the full install and uninstall, `uat:compat`, and the block node scenarios that migrate hostPath storage) cannot run on Kind. They need the separate privileged container or hosted runner lane in the table above.
- Splitting runs by lane needs finer test tags or capability gates than `integration`. That is test infrastructure work and does not depend on this design.
- Block node on Kind depends on the StorageClass storage path (delivery step 4).

Order:

1. **Baseline first.** Record the current duration of each PR job (`integration`, `compat`, `lifecycle`, `purge-storage`, `plugin-preset-upgrade`) and the VM minutes used, so the gain can be measured. No timings have been taken yet.
2. **Kind lane, first candidates.** These need only the API and do not wait for block node storage: the kube client integration tests (`TestWithCluster_*`), the Helm manager and cluster checker tests, and the alloy, teleport cluster, solo-operator and ESO scenarios. Whether a scenario belongs here is confirmed one by one.
3. **Block node on Kind.** The plugin-preset upgrade scenario first, once external mode and StorageClass storage work for block node. Scenarios that touch hostPath stay host-level.
4. **Host-level lane and tag split.** This can run in parallel with steps 2 and 3. Move the host-level tests to a privileged container or hosted runner. KVM is no longer part of the default PR gate. It starts in three ways:
   - **Path filter (automatic).** The PR touches files whose behaviour can only be verified on a real VM. The repo already has this mechanism: the `changes` job in `flow-pull-request-checks.yaml` uses `dorny/paths-filter`. Starting candidates: `internal/network/**` and the network unit workflows, `internal/templates/files/**` for sysctl, kubeadm, CRI-O and Cilium, the startup migrations (`internal/workflows/migration_*.go`, `internal/state/migration_*.go`), `internal/mount/**`, `pkg/kernel/**`, `pkg/os/**`, the daemon and its systemd unit, the release pipeline, and `taskfiles/uat.yaml`. The exact list is chosen when the lane is built.
   - **By hand.** A maintainer starts it for any other PR, through a manual workflow dispatch or a PR label.
   - **Nightly, always.** It is the safety net: a change that the path filter missed is caught within a day.

   A filter that is too broad costs KVM minutes, so keep the list tight and review it from time to time. The tag split above tells the filter which tests to run on KVM, so it does not rerun everything.

## 11. Delivery plan

1. Resolver, `--kubeconfig`, `--context`, `--cluster-name`, and the provenance gate (including the handling of older installs in section 5.4). Unit-testable on macOS with fake kubeconfigs. Update `docs/reference/global-flags.md`.
2. Cluster-side lock (`internal/kube/lease`) and its use in the mutating commands (section 5.5). It needs the resolved cluster from step 1 and nothing from the state work.
3. State layout, **after** #1231 / #1235 and #1245 merge: `state-hosts-layout` migration, the single path resolver, `provisioner.yaml`, `hosts/local/host.yaml`, the cluster-scoped component files for `clusters/<uid>/`, the host-bound field guard in section 6.2, `clusters/index.yaml`, and the `kube cluster connect`, `list` and `info` commands. This step also gives the daemon write access to its own section of `provisioner.yaml` under the #1235 lock (section 6.1); the daemon is a separate binary, so that change ships on its release schedule, not the CLI's.
4. External-mode workflows, block node first, with the StorageClass storage path.
5. CI: Kind lane using `--kubeconfig`, and the test split by required privilege.

## 12. Decisions

1. **What each command needs.** Each command declares a need (`none`, `connect`, `install`, `host`, `api` or `host+api`), and the gate checks only that (section 5.3). Diagnostic commands run the gate in report-only mode.
2. **Uninstall and reinstall.** The normal path is at uninstall: `solo-provisioner kube cluster uninstall` moves the cluster-scoped component files (`cluster`, `blocknode`, `consensus` and the cluster half of `teleport`) to `hosts/local/archive/<uid>-<timestamp>/`. Host-scoped components (software, firewall, shaping) stay, because the machine is still provisioned. It also clears the cluster ID recorded in `host.yaml`. A later `solo-provisioner kube cluster install` then starts clean. Nothing is deleted.

   If `solo-provisioner kube cluster install` finds that `hosts/local/host.yaml` records a cluster ID that differs from the live one (or a cluster that no longer exists), it **stops and does not touch the state**. Weaver cannot tell why: a `kubeadm reset` by hand, a failed or older uninstall, a rebuilt VM with surviving state, or the wrong kubeconfig all look the same, so moving the state automatically would be a guess. The error names the recorded and live cluster IDs and tells the operator to review and clean up the state by hand. The hint lists the files to move or remove and the paths they live in.

3. **Cluster-side lock.** A Lease per component per cluster, in the component's namespace, in both modes, built on `leaderelection` (section 5.5).
4. **External state is a local cache.** Almost all of it can be re-derived from the cluster, and the cluster wins when they disagree. Storing state in the cluster is a follow-up with its own design (RBAC, ownership).
5. **Garbage collection.** Nothing is deleted automatically, since a cluster that is only unreachable for a moment must not lose its state. A later `solo-provisioner state prune` lists clusters that are unreachable or whose ID no longer exists, and asks before deleting.
6. **Older installs.** No separate backfill. The gate checks binaries against the catalog checksum and writes the provision record (cluster ID, server address and CA fingerprint) on its first successful pass (section 5.4).
7. **macOS.** There is no provisioned host on darwin, so `hosts/local` is never created and every cluster is `external`. Provisioned-only commands fail with a message that they configure a Linux host. State, including `provisioner.yaml`, lives in `~/.solo-weaver/state`.

8. **Cluster registration.** One explicit command, `solo-provisioner kube cluster connect`, registers a cluster. Non-interactive runs register implicitly. `list` and `info` read the index and the cluster. `disconnect` is later work (section 4.1).

9. **External mode is experimental and provisioner-test-only.** External mode exists to test `solo-provisioner` against a throwaway cluster (such as Kind) without a KVM or privileged runner. It is not supported for running real deployments, and we do not encourage operators to use it in their own CI or clusters. It is gated with `common.GateExperimental` (feature `external-cluster`): `--kubeconfig` against a non-provisioned cluster, and `solo-provisioner kube cluster connect`, both require `--experimental` or `SOLO_PROVISIONER_ENABLE_EXTERNAL_CLUSTER`, and are refused otherwise. The provisioned single-node Linux path is unaffected and needs no flag.

## 13. Still open

- What `solo-provisioner kube cluster uninstall` does with state today. This decides whether archive-at-uninstall is new behaviour or a fix.
- Whether to offer explicit commands so operators do not edit state by hand: one for cleanup after a stale-state stop, and one to discard or adopt external state for a cluster that now passes the local proof (section 5.2), for example `solo-provisioner state adopt`.
- Details of `solo-provisioner kube cluster info` (which component states it shows) and of `disconnect` (what it does with state files). Both belong with the diagnostics work (#1110) and the prune command.
- Whether the writer guard in section 6.2 is enough, or the files should be split by owner, and whether that belongs in #1245's follow-ups or in this work.
- Where the action history (`LastAction` and `action_history.yaml`, #1244) lives. It is probably per target (host or cluster), but that needs a look.
- Agreeing the `ProvisionerState` and `Version` move out of `system` with #1245.
- The lease timings, and the name of the explicit override flag for removing a dead lease.
- The detail of report-only mode for `doctor` and `diagnose` (epic #1110).
- The design of in-cluster state storage, if we want it. Prior art in section 14: a labelled ConfigMap with `lastUpdatedBy` and `lastUpdatedAt` (the lease holder identity already carries user, host and command), and a validator that checks the recorded components against the live cluster.
- Whether a grouping of clusters is ever needed (Solo's deployments span several clusters). It may matter for consensus orbits that span clusters. Out of scope here.
- The `solo-provisioner state prune` command and its safety rules.

## 14. Prior art: hiero-ledger/solo

Solo (`hiero-ledger/solo`) solves part of this problem. We do not adopt its code, but we use some of its ideas.

- **Alias to context map.** A Solo "cluster reference" is a user-chosen alias mapped to a kubeconfig context name, kept in a local config file. Commands: `cluster-ref config connect | disconnect | list | info`. `connect` first tests the connection. This is the model behind `kube cluster connect`, `list` and `info` in section 4.1.
- **State in the cluster.** Solo keeps component state in a labelled ConfigMap (`solo-remote-config`) with history and `lastUpdatedAt` and `lastUpdatedBy`, and a validator checks the recorded components against the live cluster (Helm releases, pods). That is the shape of the later in-cluster state work in section 13.
- **Deployments.** A Solo deployment groups a namespace and one or more cluster aliases. We leave that out for now.
- **What we avoid.** The alias is Solo's only identity. Nothing checks which cluster a context really points at, `connect` can silently remap an alias, and kubeconfig context names are mutable. Here the key is the cluster ID, an alias binds to one ID, and rebinding needs `--rebind`. Solo also has no notion of whether a cluster was installed by Solo.
- **Lease.** Solo's lock is a Lease with a holder identity, renewed in the background. Section 5.5 adapts it.
