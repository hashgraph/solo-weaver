# Bring Your Own Cluster and Per-Cluster State

Status: **Draft for review.** Nothing in this document is implemented yet.

## 1. Problem

`solo-provisioner` assumes one machine runs exactly one cluster, and that the cluster is the one it installed. The code does not check this.

- `kube.ProbeCluster()` (`internal/kube/client.go`) only asks if a kubeconfig is reachable. It reads `$KUBECONFIG` or `~/.kube/config`, stats the file, and calls `/version` with a 3 second timeout.
- `clusterChecker.RefreshState` then sets `ClusterState.Created = true` for any reachable cluster.
- `bll/blocknode` and `bll/consensus` install handlers branch on `Created`. A block node install can run against whatever cluster the operator's shell points at, and still run host-level steps (firewall, tc shaping, bind mounts, weaver user).
- Kubeconfig resolution is duplicated in `loadKubeConfig`, `resolveKubeconfigPath`, `step_daemon_provision.newTypedClient`, Helm's own settings, the daemon (`kubeconfigPath` in six places), and two hard-coded `/etc/kubernetes/admin.conf` constants.
- All state lives in one file, `/opt/solo/weaver/state/state.yaml`, so a second cluster would overwrite the first one's state.

We also want to run most UAT against a cluster we did not install (for example Kind), so CI does not need KVM or privileged runners for every test.

## 2. Goals and non-goals

Goals:

1. Never trust a cluster as ours unless we can prove we installed it on this machine. Check this as early as possible, and stop if the proof fails.
2. Let an operator explicitly point the CLI at any cluster (`--kubeconfig`) and use it for API-level work: install, upgrade and uninstall of block node, consensus node and the other cluster components.
3. Keep state separate per cluster, so switching between clusters, or forgetting a flag, can never mix or reset state.
4. One place decides which kubeconfig is used.
5. Leave room to manage several VMs from macOS later.

Non-goals for now:

- SSH delegation to remote hosts (`--host`). See section 9.
- Storing state inside the cluster.
- A full darwin build of the CLI.

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
| `--cluster-name <alias>` | Optional human label, stored in `clusters/index.yaml`. Never used as a key. |

Without `--kubeconfig`, ambient `$KUBECONFIG` and `~/.kube/config` are not trusted on their own. Weaver already copies `admin.conf` to `~/.kube/config` for root and the sudo user (`internal/kube/admin.go`), so that file can be ours, but the identity check, not the path, proves it.

## 5. Identity gate

A check in the root pre-run, **before** any state load, RSL or reality check. If it fails, the command stops with a reason code and a hint. This is how we know reality and state can be trusted.

### 5.1 What a `provisioned` cluster must prove

All of these must pass:

| Evidence | Why |
|---|---|
| Software state records kubelet and CRI-O as installed, and the sandbox binaries on disk match the recorded hash | Binary provenance |
| kubelet and CRI-O are running (systemd unit active, CRI-O socket answers), and the running executable is the sandbox binary, not one from `PATH` | A matching file is not enough |
| `/etc/kubernetes/admin.conf` and the static pod manifests under `/etc/kubernetes/manifests` exist | kubeadm control plane evidence |
| The kubeconfig server host matches this machine's IP and the IP in weaver's config | The cluster is on this host |
| The live cluster ID matches the one recorded in state (when one is recorded) | Same cluster as last time |

The IP check alone is not enough. A Kind or minikube cluster on the same machine points at `127.0.0.1` and would pass it. The binary and process checks rule those out.

### 5.2 Outcomes

| Situation | Result |
|---|---|
| All checks pass | `provisioned`, host `local`, state in `hosts/local` |
| Checks fail, `--kubeconfig` given | `external`, state in `clusters/<uid>` |
| Checks fail, no `--kubeconfig` | Stop. Hint: run `solo-provisioner kube cluster install`, or pass `--kubeconfig` for an external cluster |
| Cluster ID matches the one recorded in `hosts/local`, but a local check fails (for example kubelet is down) | Stop. Hint: fix the local cluster. **Never** fall back to `external` |
| Cluster ID already has `clusters/<uid>` state and now passes the local proof | Stop. Needs an explicit step; it is never adopted silently |

The last two rows prevent split-brain between the two state locations.

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

Old installs may lack the hashes or metadata the gate checks, and a strict gate would lock operators out of their own clusters. A one-time backfill records the missing metadata and the cluster ID when the weaker but still conclusive checks pass: local sandbox kubelet and CRI-O running, `admin.conf` present, IP match. This must ship together with the gate.

## 6. State layout

State is grouped by host. A cluster with no host on this machine gets its own directory.

```
/opt/solo/weaver/state/
├── hosts/
│   └── local/state.yaml           # this machine + the cluster it provisioned (content unchanged)
└── clusters/
    ├── <kube-system-uid>/state.yaml   # one per cluster with no host here; API-only state type
    └── index.yaml                     # complete view of every known cluster
```

Rules:

- `hosts/local/state.yaml` keeps today's content: machine, software, hardware, firewall, shaping, and the provisioned cluster's workloads. It also records the provisioned cluster's ID.
- `clusters/<uid>/state.yaml` uses a **separate type with API-level fields only** (cluster info, Helm releases, block node release and plugin settings, consensus node specs, teleport cluster agent). It has no machine, firewall, shaping or hostPath fields, so external-mode code cannot touch host state by accident.
- Each file records its own `clusterId` and `origin`, and the loader checks them against the live cluster. A copied file is rejected.
- External runs never write `hosts/local`. Provisioned runs never write `clusters/<uid>`.
- The key is the cluster ID, not the IP, the context name or a user-supplied name. A rebuilt cluster gets a new ID and therefore fresh state.

### 6.1 `clusters/index.yaml`

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

- Weaver did not install it, so it is `origin: external`. It gets `clusters/<kube-system-uid>/state.yaml` and an index entry with the context (for example `kind-dev`) and a `127.0.0.1` server address.
- Use it with `--kubeconfig` and `--context`. Only API-level work runs. Storage comes from Kind's default StorageClass.
- Each `kind create cluster` gets a new cluster ID, so a recreated cluster starts with clean state. If the same alias is reused for the new cluster, `--rebind` is needed.
- The same flow is what the CI Kind lane uses (section 10).

Prerequisites for running this on macOS:

- The CLI must run on darwin. Build stubs for the Linux-only code (`mount`, `modprobe`, swap) exist, but running the CLI on macOS has not been verified.
- The state directory defaults to `~/.solo-weaver/state` on macOS, because `/opt/solo/weaver` is a Linux path.

## 8. Migration of existing deployments

Add a startup migration, `state-hosts-layout`, registered first in `RegisterMigrations()` (`cmd/cli/commands/root.go`). See `docs/dev/migration-framework.md`.

- `Applies`: `state/state.yaml` exists and `state/hosts/local/state.yaml` does not.
- `Execute`: create `hosts/local/` with the same owner and mode, copy the file, verify the hash, rename atomically, and keep the original as `state.yaml.pre-hosts-layout`.
- If both files exist, prefer the new one, leave the legacy one untouched, and log a warning. Never overwrite.
- Idempotent and crash-safe. `Rollback` restores the backup on a best-effort basis.
- It runs before anything opens the state manager, and before the identity gate. Migration is pure filesystem work and needs no cluster.
- All code that builds the state path (`NewState`, `state_reader.go`, the `helm-release-schema-v2` and `mgmt-ports-v1` migrations, `testutil`) moves to one resolver.

Risks to settle:

1. The `StateFile` field is serialized inside `state.yaml`. After the move it must be rewritten, and the loader should force it to the path actually loaded, or a later flush could write to the old location.
2. Downgrade: an older binary looks at the old path and sees a fresh machine. Migrations are one-way today; document it. A symlink does not help, because the atomic write replaces it.
3. The daemon is a separate binary. Check whether it reads the state path.
4. Two concurrent invocations during the move need a lock, or an atomic rename plus a re-check.
5. The legacy-cluster backfill in section 5.4 ships with this change.

## 9. Later: several VMs from macOS

Not in scope now, but the layout leaves room.

- A developer on macOS runs a launcher that copies the matching Linux binary to a VM, verifies its digest, and runs the command there over SSH. The VM keeps its own `hosts/local` state.
- The Mac keeps `hosts/<name>/host.yaml` as a registry only: SSH target, verified machine ID, last known binary version. It never holds a copy of a VM's state.
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

1. Resolver, `--kubeconfig`, `--context`, `--cluster-name`, and the identity gate (including the legacy backfill rules). Unit-testable on macOS with fake kubeconfigs. Update `docs/reference/global-flags.md`.
2. State layout: `state-hosts-layout` migration, the single path resolver, the external state type and `clusters/index.yaml`.
3. External-mode workflows, block node first, with the StorageClass storage path.
4. CI: Kind lane using `--kubeconfig`, and the test split by required privilege.

## 12. Decisions

1. **Gate exemptions.** The list in section 5.3 is the rule. Diagnostic commands run the gate in report-only mode.
2. **Reinstall.** When `solo-provisioner kube cluster install` finds that `hosts/local` records a different cluster ID, the old cluster section moves to `hosts/local/archive/<old-uid>-<timestamp>.yaml`. Host-scoped data (software, firewall) stays. Nothing is deleted, and the new cluster starts with an empty cluster section.
3. **External state is a local cache.** Almost all of it can be re-derived from the cluster, and the cluster wins when they disagree. Storing state in the cluster is a follow-up with its own design (RBAC, ownership).
4. **Garbage collection.** Nothing is deleted automatically, since a cluster that is only unreachable for a moment must not lose its state. A later `solo-provisioner state prune` lists clusters that are unreachable or whose ID no longer exists, and asks before deleting.
5. **macOS.** There is no provisioned host on darwin, so `hosts/local` is never created and every cluster is `external`. Provisioned-only commands fail with a message that they configure a Linux host. State lives in `~/.solo-weaver/state`.

## 13. Still open

- The detail of report-only mode for `doctor` and `diagnose` (epic #1110).
- The design of in-cluster state storage, if we want it.
- The `solo-provisioner state prune` command and its safety rules.
