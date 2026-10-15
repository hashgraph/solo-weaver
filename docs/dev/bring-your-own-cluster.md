# Bring Your Own Cluster and Per-Cluster State

Status: **Draft for review.** Nothing in this document is implemented yet.

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
2. Let an operator explicitly point the CLI at any cluster (`--kubeconfig`) and use it for API-level work: install, upgrade and uninstall of block node, consensus node and the other cluster components.
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

The mode is **derived, not declared**. The `--kubeconfig` flag only chooses which cluster to look at. The identity check (section 5) decides whether it is `provisioned` on this host or `external`. This is what stops flag flip-flopping from corrupting state.

## 4. Cluster resolution

Add one resolver, `kube.ResolveCluster(opts)`. It returns the REST config, the kubeconfig path, the context, the cluster ID and the origin. Every caller listed in section 1 uses it.

New root persistent flags:

| Flag | Meaning |
|---|---|
| `--kubeconfig <path>` | Kubeconfig to use. Without it, only the cluster this host provisioned is considered. |
| `--context <name>` | Context inside the kubeconfig. |
| `--cluster-name <alias>` | Optional human label, stored in `clusters/index.yaml`. Defaults to the context name. Never used as a key. |

Without `--kubeconfig`, ambient `$KUBECONFIG` and `~/.kube/config` are not trusted on their own. Weaver already copies `admin.conf` to `~/.kube/config` for root and the sudo user (`internal/kube/admin.go`), so that file can be ours, but the identity check, not the path, proves it.

## 5. Identity gate

A check in the root pre-run, **before** any state load, RSL or reality check. If it fails, the command stops with a reason code and a hint. This is how we know reality and state can be trusted.

### 5.1 What a `provisioned` cluster must prove

All of these must pass:

| Evidence | Why |
|---|---|
| Software state records kubelet and CRI-O as installed, and the sandbox binaries on disk match the catalog checksum for the recorded version and platform | Binary provenance |
| kubelet and CRI-O are running (systemd unit active, CRI-O socket answers), and the running executable is the sandbox binary, not one from `PATH` | A matching file is not enough |
| `/etc/kubernetes/admin.conf` and the static pod manifests under `/etc/kubernetes/manifests` exist | kubeadm control plane evidence |
| The kubeconfig server host matches this machine's IP and the IP in weaver's config | The cluster is on this host |
| The live cluster ID matches the one recorded in `hosts/local/host.yaml` (when one is recorded; see 5.4) | Same cluster as last time |

The IP check alone is not enough. A Kind or minikube cluster on the same machine points at `127.0.0.1` and would pass it. The binary and process checks rule those out.

### 5.2 Outcomes

| Situation | Result |
|---|---|
| All checks pass | `provisioned`, host `local`, state in `hosts/local` |
| Checks fail, `--kubeconfig` given | `external`, state in `clusters/<uid>` |
| Checks fail, no `--kubeconfig` | Stop. Hint: run `solo-provisioner kube cluster install`, or pass `--kubeconfig` for an external cluster |
| Cluster ID matches the one recorded in `hosts/local/host.yaml`, but a local check fails (for example kubelet is down) | Stop. Hint: fix the local cluster. **Never** fall back to `external` |
| Cluster ID already has `clusters/<uid>` state and now passes the local proof | Stop. Needs an explicit step; it is never adopted silently |

The last two rows prevent split-brain between the two state locations.

#### The last row: a cluster that was external and now passes the local proof

This can happen when a cluster on the machine was not recognised at first (for example kubelet was down, or it was built by hand), so a command with `--kubeconfig` treated it as `external` and created `clusters/<uid>/`. Later the proof passes and the same cluster ID now qualifies as `provisioned`. Two places could then describe one cluster, and the records may disagree. Picking one silently could ignore the block node and consensus information in the other, and merging automatically could overwrite newer facts with older ones. So weaver stops and asks.

The explicit step is one of:

1. **Discard the external copy.** The operator archives or removes `clusters/<uid>/` and reruns. External state is a cache (decision 4), so `hosts/local` is rebuilt from what the cluster reports.
2. **Adopt it.** A command copies the cluster-scoped component files (`cluster`, `blocknode`, `consensus` and the cluster half of `teleport`) from `clusters/<uid>/` into `hosts/local/`, then archives the external directory.

Until a command for this exists (section 13), the error lists the manual steps.

### 5.3 Exempt commands

These commands expect no cluster yet, so they skip the gate:

- `solo-provisioner install`
- `solo-provisioner uninstall`
- `solo-provisioner version`
- `solo-provisioner demo`
- `solo-provisioner --help` (and `--help` on any subcommand)
- `solo-provisioner kube cluster install`

`solo-provisioner kube cluster install` still has to refuse to proceed when a foreign cluster is configured.

Diagnostic commands (`doctor` and `diagnose`, epic #1110) are **not** exempt, because their job is to report on the cluster. They run the identity check in **report-only mode**: a failed check is printed as a finding instead of stopping the command. The detail of those commands belongs to #1110.

### 5.4 Clusters installed by older versions

A strict gate must not lock operators out of clusters that older versions installed. Two things could be missing on such a machine, and neither needs a separate migration:

- **Recorded hashes.** Software state records only the name, version, `Installed` and `Configured`; it has never held a binary hash. The gate does not need one. The embedded catalog has the expected checksum for each software, version and platform, which the installer already checks at download. The gate verifies that the sandbox binary on disk matches the catalog checksum for the version recorded in state, and that the running process is that binary.
- **The cluster ID.** `hosts/local/host.yaml` has none on an older install. The first time all the checks in 5.1 pass, the gate records the live cluster ID. This is trust on first use, and it is safe because the binary, process, `admin.conf` and IP checks have already passed.

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
- **Fencing.** If renewal fails past the renew deadline, the wrapper cancels the workflow's context and the command aborts. A laptop that sleeps past expiry can therefore not resume and keep changing a cluster another operator now holds.
- **Expiry only.** The lease is taken over only after it expires, because a live holder keeps renewing. There is no other automatic takeover. An explicit override flag (name to be decided) lets an operator remove a lease they know is dead.
- **Timings.** Starting values: lease duration 30 seconds, renew deadline 20 seconds, retry period 5 seconds. To be tuned against slow API servers.
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
├── provisioner.yaml               # binary-level: provisioner version, commit, executable, state model version
├── hosts/
│   └── local/                     # this machine + the cluster it provisioned
│       ├── host.yaml              # identity: name, machine ID, the cluster this host provisioned
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

- `provisioner.yaml` describes the binary, not a host or a cluster. It is always loadable, the binary reads it first before choosing a host or a cluster, and it exists on every machine, including a Mac with only external clusters (`~/.solo-weaver/state/provisioner.yaml`). Startup migrations depend on the recorded provisioner version, and without this file a machine with no `hosts/local` would have nowhere to record it. Host and cluster state never write it.
- `hosts/local/host.yaml` is a small identity file that the identity gate reads first, before any component file, so it needs no component lock. It holds the host `name`, `kind` (`local`; `remote` later), the `machineId` from `/etc/machine-id`, and `managedClusterId`, the cluster this host provisioned (empty when none). A later remote host (section 9) uses the same file with extra registry fields. If the recorded `machineId` no longer matches the machine, for example because the directory was copied, the command stops. If the machine has no usable `/etc/machine-id` (some containers), the check is skipped and no ID is recorded.
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

The alias binds to a cluster ID on first use. If the alias later points at a different cluster ID, the command stops with an error. `--rebind` overrides it explicitly. An alias alone can never create state.

## 7. External mode: API only

> In external mode weaver only talks to the Kubernetes API. It touches nothing on the host.

- Skipped: node setup, kubeadm setup, network setup (nft, tc), bind mounts, sysctl, swap, weaver user, daemon config.
- Storage: hostPath PVs refer to a node filesystem weaver cannot own, so they are not used. Storage comes from the cluster through a `storageClassName` per volume (flag or values file). Weaver creates PVCs only, never PVs. The hostPath templates (`storage-config.yaml`, `optional-storage.yaml`) are provisioned-only.
- Preflight checks API-level facts only: server version range, access via `SelfSubjectAccessReview`, and that the CNI and StorageClass exist.
- Provisioned-only commands fail in external mode with a reason code and hint: `solo-provisioner kube cluster install`, `solo-provisioner kube cluster uninstall`, `solo-provisioner teleport node ...`, and `solo-provisioner network firewall|shape|policy ...`.
- Uninstall and reset remove only what weaver created in that cluster, tracked by labels or state. Cluster-level components are never touched.
- This also applies on a provisioned host: pointing it at a remote cluster with `--kubeconfig` runs external mode and skips the host steps.

### 7.1 Local development with Kind

A developer can create a Kind cluster on their own machine (including a Mac) and use it with weaver. It does not need SSH delegation (section 9).

- Weaver did not install it, so it is `origin: external`. It gets a `clusters/<kube-system-uid>/` directory and an index entry with the context (for example `kind-dev`) and a `127.0.0.1` server address.
- Use it with `--kubeconfig` and `--context`. Only API-level work runs. Storage comes from Kind's default StorageClass.
- Each `kind create cluster` gets a new cluster ID, so a recreated cluster starts with clean state. If the same alias is reused for the new cluster, `--rebind` is needed.
- The same flow is what the CI Kind lane uses (section 10).

Prerequisites for running this on macOS:

- The CLI must run on darwin. Build stubs for the Linux-only code (`mount`, `modprobe`, swap) exist, but running the CLI on macOS has not been verified.
- The state directory defaults to `~/.solo-weaver/state` on macOS, because `/opt/solo/weaver` is a Linux path.

## 8. Migration of existing deployments

Add a startup migration, `state-hosts-layout`, registered in `RegisterMigrations()` (`cmd/cli/commands/root.go`) **after** the migration that splits `state.yaml` into component files (#1231). See `docs/dev/migration-framework.md`. The chain is: `unified-state`, then per-component split, then `state-hosts-layout`. Each step is idempotent.

- `Applies`: component files exist directly under `state/` and `state/hosts/local/` does not.
- `Execute`: create `hosts/local/` with the same owner and mode, move the component files (and their lock and hash companions) into it, and keep a backup of the originals as `state/.pre-hosts-layout/`. It moves files and does not rewrite their content, with one exception: it copies `ProvisionerState` and `Version` out of the machine-level file into `state/provisioner.yaml`, and writes `hosts/local/host.yaml` (name, kind, machine ID, and the cluster ID if one is already recorded).
- If both locations have files, prefer `hosts/local/`, leave the legacy files untouched, and log a warning. Never overwrite.
- Idempotent and crash-safe. `Rollback` restores the backup on a best-effort basis.
- It runs before anything opens the state manager, and before the identity gate. Migration is pure filesystem work and needs no cluster.
- All code that builds a state path (the state manager, `state_reader.go`, the existing state migrations, `testutil`, the daemon) moves to one resolver.

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

External mode is what makes a Kind lane possible on the ARC runners (`solo-linux-medium`) without privileged pods. Tests then split into:

| Lane | Runner | Content |
|---|---|---|
| Unprivileged | `solo-linux-medium` | Unit tests, installer download and verify tests |
| Kind (external mode) | `solo-linux-medium` with `dind` | API-level UAT: chart and plugin-preset upgrade, kube client, alloy, teleport cluster, solo-operator |
| Host-level (provisioned) | Hosted runner or privileged systemd container | Network, mount, sysctl, systemd tests and the full cluster install UAT |
| Real VM | `kvm-runner`, nightly or on path filters | Boot-order and reboot tests, one full Debian lifecycle |

## 11. Delivery plan

1. Resolver, `--kubeconfig`, `--context`, `--cluster-name`, and the identity gate (including the handling of older installs in section 5.4). Unit-testable on macOS with fake kubeconfigs. Update `docs/reference/global-flags.md`.
2. Cluster-side lock (`internal/kube/lease`) and its use in the mutating commands (section 5.5). It needs the resolved cluster from step 1 and nothing from the state work.
3. State layout, **after** #1231 / #1235 and #1245 merge: `state-hosts-layout` migration, the single path resolver, `provisioner.yaml`, `hosts/local/host.yaml`, the cluster-scoped component files for `clusters/<uid>/`, the host-bound field guard in section 6.2, and `clusters/index.yaml`.
4. External-mode workflows, block node first, with the StorageClass storage path.
5. CI: Kind lane using `--kubeconfig`, and the test split by required privilege.

## 12. Decisions

1. **Gate exemptions.** The list in section 5.3 is the rule. Diagnostic commands run the gate in report-only mode.
2. **Uninstall and reinstall.** The normal path is at uninstall: `solo-provisioner kube cluster uninstall` moves the cluster-scoped component files (`cluster`, `blocknode`, `consensus` and the cluster half of `teleport`) to `hosts/local/archive/<uid>-<timestamp>/`. Host-scoped components (software, firewall, shaping) stay, because the machine is still provisioned. It also clears the cluster ID recorded in `host.yaml`. A later `solo-provisioner kube cluster install` then starts clean. Nothing is deleted.

   If `solo-provisioner kube cluster install` finds that `hosts/local/host.yaml` records a cluster ID that differs from the live one (or a cluster that no longer exists), it **stops and does not touch the state**. Weaver cannot tell why: a `kubeadm reset` by hand, a failed or older uninstall, a rebuilt VM with surviving state, or the wrong kubeconfig all look the same, so moving the state automatically would be a guess. The error names the recorded and live cluster IDs and tells the operator to review and clean up the state by hand. The hint lists the files to move or remove and the paths they live in.

3. **Cluster-side lock.** A Lease per component per cluster, in the component's namespace, in both modes, built on `leaderelection` (section 5.5).
4. **External state is a local cache.** Almost all of it can be re-derived from the cluster, and the cluster wins when they disagree. Storing state in the cluster is a follow-up with its own design (RBAC, ownership).
5. **Garbage collection.** Nothing is deleted automatically, since a cluster that is only unreachable for a moment must not lose its state. A later `solo-provisioner state prune` lists clusters that are unreachable or whose ID no longer exists, and asks before deleting.
6. **Older installs.** No separate backfill. The gate checks binaries against the catalog checksum and records the cluster ID on its first successful pass (section 5.4).
7. **macOS.** There is no provisioned host on darwin, so `hosts/local` is never created and every cluster is `external`. Provisioned-only commands fail with a message that they configure a Linux host. State, including `provisioner.yaml`, lives in `~/.solo-weaver/state`.

## 13. Still open

- What `solo-provisioner kube cluster uninstall` does with state today. This decides whether archive-at-uninstall is new behaviour or a fix.
- Whether to offer explicit commands so operators do not edit state by hand: one for cleanup after a stale-state stop, and one to discard or adopt external state for a cluster that now passes the local proof (section 5.2), for example `solo-provisioner state adopt`.
- A read-only `solo-provisioner kube cluster list` that prints `clusters/index.yaml` (section 6.3).
- Whether the writer guard in section 6.2 is enough, or the files should be split by owner, and whether that belongs in #1245's follow-ups or in this work.
- Where the action history (`LastAction` and `action_history.yaml`, #1244) lives. It is probably per target (host or cluster), but that needs a look.
- Agreeing the `ProvisionerState` and `Version` move out of `system` with #1245.
- The lease timings, and the name of the explicit override flag for removing a dead lease.
- The detail of report-only mode for `doctor` and `diagnose` (epic #1110).
- The design of in-cluster state storage, if we want it.
- The `solo-provisioner state prune` command and its safety rules.
