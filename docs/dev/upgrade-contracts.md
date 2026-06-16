# Upgrade Contracts (self-upgrade.yaml, .bak naming, CR phase handshake)

This page pins three contracts shared across two parallel tracks:

- **Epic #502 — Network Upgrade (Execute Phase):** the daemon-side execute
  workflow that reacts to `NetworkUpgradeExecute` CRs.
- **Epic #500 — Self-Upgrade Protocol:** the daemon upgrading its own binaries.

They are defined once so both tracks build against the same definitions rather
than redefining them. This is a types + constants + docs contract; it carries no
business logic.

**It can be deleted once the features are implemented and the contracts are stable.** The contracts themselves live in `internal/consensus` and `internal/selfupgrade` (see below); this page is the single source of truth for their design and intended use

| Contract | Package | Consumed by |
|---|---|---|
| CR phase + `DaemonResult` condition constants | `internal/consensus` | #706, #708, #709, #717, #500 spawn/recover |
| `self-upgrade.yaml` schema | `internal/selfupgrade` | #529 (writer), #717 (recover) |
| `.bak` binary naming convention | `internal/selfupgrade` | #526 (archive), #528 / #717 (recover) |

## Package placement & the import rule

`internal/consensus` holds consensus-node contracts shared between the daemon
(`internal/daemon/consensus`) and the CLI/BLL. Both are named `package consensus`,
so a single file must **never import both** — daemon implementation files import
`internal/consensus` under an alias (`cn`); CLI/BLL files import it directly.
This one-way dependency keeps the build cycle-free.

`internal/selfupgrade` is a separate package because the self-upgrade state file
and `.bak` archives concern the provisioner upgrading **its own binaries**, not a
consensus node. Keeping it out of `internal/consensus` avoids a category error and
lets the CLI self-upgrade code import it without dragging in consensus types.

## 1. NetworkUpgradeExecute CR phase + condition handshake

Defined in `internal/consensus/phase.go` as the `Phase`, `ConditionType`, and
`ConditionStatus` typed string constants.

### Writer ownership invariant (finalized in #706)

- The **reconciler is the sole writer of terminal phases** `Succeeded` and `Failed`.
- The **daemon writes only two phases**:
  - `PendingInfraUpgrade` — written **durably, before any infra-mutating work**.
    This is the single crash-recovery resume anchor: on restart the daemon
    re-reads the CR, sees this phase, and resumes the infra upgrade from here.
  - `PendingNodeUpgrade` — the daemon's final write, **after** it sets the
    `DaemonResult` condition, handing control back to the reconciler.
- There is **no `InProgress` phase**. Progress is not modelled as a phase; the
  daemon reports its outcome via the `DaemonResult` condition (`True`/`False`),
  and the reconciler maps that to the terminal phase.

`Phase.IsTerminal()` and `Phase.IsDaemonWritable()` codify the invariant in code;
the unit tests assert that no phase is both (the daemon can never write a terminal
phase) and that no phase is named `InProgress`.

### State machine

```
        reconciler              reconciler                 daemon (durable)
  ┌──────────────────┐   ┌────────────────────────┐   ┌──────────────────────┐
  │     Pending      │──▶│ ReadyForProvisionerDaemon│─▶│  PendingInfraUpgrade  │
  └──────────────────┘   └────────────────────────┘   └──────────┬───────────┘
                                                                  │ infra work
                                                                  │ done
                                                                  ▼
                                              daemon sets DaemonResult condition
                                                  (True on success, False on
                                                   failure / recovered panic)
                                                                  │
                                                                  ▼
                                                   ┌──────────────────────────┐
                                                   │    PendingNodeUpgrade     │ (daemon)
                                                   └──────────────┬───────────┘
                                                                  │ reconciler reads
                                                                  │ DaemonResult
                                            DaemonResult=True      │      DaemonResult=False
                                          ┌───────────────────────┴───────────────────────┐
                                          ▼                                                 ▼
                                ┌──────────────────┐                            ┌──────────────────┐
                                │    Succeeded     │ (reconciler)               │      Failed      │ (reconciler)
                                └──────────────────┘                            └──────────────────┘
```

- **Resume:** a crash anywhere after `PendingInfraUpgrade` is written resumes from
  that phase, because the phase lives durably in etcd (#709 / #717).
- **Failure path:** the daemon never writes `Failed`; it sets `DaemonResult=False`,
  transitions to `PendingNodeUpgrade`, and the reconciler writes `Failed`.

## 2. self-upgrade.yaml schema

Defined in `internal/selfupgrade/selfupgrade.go`. Lives at the HIP-authoritative
path `WeaverPaths.SelfUpgradeYAMLPath` = `/opt/solo/weaver/daemon/self-upgrade.yaml`.

The **old daemon writes this file at step 0** — before spawning the detached
upgrade process and before any binary swap begins. It is both a handoff record and
a crash-diagnostic artifact:

- **Crash diagnostics** — if the detached upgrader dies mid-swap, the recover tool
  reads it to learn the target versions, the last step reached, and whether the
  `.bak` binaries are still present.
- **PID tracking** — `childPid` lets a tool check whether the detached process is
  still alive (`kill -0`).
- **Recovery input** — `solo-provisioner consensus node upgrade-recover` (#717)
  inspects it to decide whether to restore a `.bak` binary and restart the daemon.

Because it is written *before* the swap, a leftover `status: in-progress` is itself
the failure signal — a clean run always ends `succeeded`. On failure the detached
process writes `failed` and leaves the `.bak` files intact.

### Fields

```yaml
schemaVersion: 1                 # strict; an unsupported version is rejected
timestamp: 2026-06-16T10:30:00Z  # RFC3339 UTC, when the operation began
operationId: op-...              # ties to NetworkUpgradeExecute spec.operationId
status: in-progress              # in-progress | succeeded | failed
childPid: 4242                   # detached upgrader PID (liveness check)
currentStep: swap-cli-binary     # last step begun (crash localisation)
fromCliVersion: v1.1.0
toCliVersion: v1.2.3
fromDaemonVersion: daemon-v1.1.0
toDaemonVersion: daemon-v1.2.3
cliBakPath: /opt/solo/weaver/backup/solo-provisioner/solo-provisioner-op-...bak
daemonBakPath: /opt/solo/weaver/backup/solo-provisioner/solo-provisioner-daemon-op-...bak
```

### Loading / saving

- `selfupgrade.Load(path)` strict-decodes (unknown fields rejected) and rejects any
  `schemaVersion` greater than this build supports.
- `selfupgrade.Save(path, s)` stamps `schemaVersion` and writes atomically
  (write-temp-then-rename) so a crash mid-write never leaves a torn file.

## 3. .bak binary naming convention

Defined in `internal/selfupgrade/bak.go`. The live binaries are installed in
`WeaverPaths.BinDir` (`/opt/solo/weaver/bin`), which is `root:root`. Their archives
are **not** kept there — they live in a dedicated backup subdirectory,
`BakDir` = `/opt/solo/weaver/backup/solo-provisioner` (under `WeaverPaths.BackupDir`),
so the bin dir holds only live executables. Both trees are under `/opt/solo/weaver`
(same filesystem as the live binary), so the swap's rename stays atomic; the
self-upgrade swap runs as root, so writing into either tree is fine.

```
solo-provisioner-<operationId>.bak          # archived CLI binary
solo-provisioner-daemon-<operationId>.bak   # archived daemon binary
```

Helpers (one source of truth for both producing and parsing):

- `BakDir(backupDir)` — the archive directory (`backupDir/solo-provisioner`).
- `CLIBakName(operationID)` / `DaemonBakName(operationID)` — produce the filename
  (`(string, error)`; reject a non-identifier-safe `operationId`).
- `CLIBakPath(bakDir, operationID)` / `DaemonBakPath(bakDir, operationID)` —
  absolute paths (`(string, error)`).
- `ParseBakName(name)` — returns the `Binary` and the embedded `operationId`.

**operationId safety:** the producers validate `operationId` via
`sanity.ValidateOperationID` (charset `[A-Za-z0-9_.-]`, non-empty, no `..`, no
shell metacharacters). A `.` is permitted so ids can embed dotted version strings
(e.g. `upgrade-v0.76.0-20060102T150405Z`), while `..`, path separators, and shell
metacharacters are rejected so a crafted id cannot escape `bakDir` after
`filepath.Join` — important because the swap runs as root. The upgrade/self-upgrade
flows consume the id from the CR's `spec.operationId`; the migration flow
constructs its own (`migration-<timestamp>`). Any constructed id must satisfy this
validator.

**Parsing gotcha:** the daemon name embeds the CLI name as a prefix
(`solo-provisioner-daemon-...` starts with `solo-provisioner-...`), so `ParseBakName`
tests the daemon prefix first to avoid misclassifying a daemon archive as a CLI
archive with an `operationId` of `daemon-...`.

## Schema versioning helper (pkg/schema)

`self-upgrade.yaml` loads via the reusable `pkg/schema` package, which captures the
versioned owned-state-file pattern for any future schema:

1. probe the version key from raw YAML,
2. normalise absent/zero to 1,
3. reject a version newer than this build supports,
4. strict-decode (KnownFields, single document) into the sealed per-version struct,
5. walk the migration chain via `Migratable[T].MigrateToLatest()`.

Each schema supplies only its sealed `vN` structs and their `MigrateToLatest`
field transforms; the orchestration is shared. The version key is configurable
via `Versioned.VersionKey` and defaults to `schemaVersion` when left empty, so
solo-weaver state files stay consistent while an external consumer can adopt any
key it prefers. `daemon.yaml` is standardised on `schemaVersion` as of the rename
in this change.

`pkg/schema` is not restricted to provisioner-owned files. It is equally suitable
for council-signed manifests: signature verification operates on the raw bytes
before `Decode` is called, so in-memory migration never touches the signed payload.
`pkg/manifests` currently uses its own `ValidateSchemaVersion` allow-list instead,
but migrating it to `pkg/schema` is a straightforward follow-up if the need arises.

## See also

- `internal/consensus/phase.go` — phase / condition constants and invariant helpers
- `internal/selfupgrade/selfupgrade.go` — self-upgrade.yaml schema, Load/Save
- `internal/selfupgrade/bak.go` — .bak naming helpers
- `pkg/schema/schema.go` — generic versioned loader
- `internal/daemon/consensus/upgrade_monitor.go` — daemon consumer of the phase constants
