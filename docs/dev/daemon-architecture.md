# solo-provisioner-daemon Architecture

> This document is for developers working on or extending the daemon.
> For operator testing instructions see [daemon-testing-guide.md](daemon-testing-guide.md).

## Overview

`solo-provisioner-daemon` is a long-running Linux systemd service that monitors a Hedera network node
for operator-triggered events (upgrades, migrations) and acts on them autonomously.
It is a **separate binary** from `solo-provisioner` (the CLI) and is installed, managed, and
queried through the CLI's `daemon service` sub-commands.

Key design principles:

- **Fail-fast startup**: the daemon refuses to start if `daemon.yaml` is missing, malformed,
  or if any enabled component's kubeconfig is unreachable.
- **Independent component kubeconfigs**: each component (consensus-node, block-node) has its own
  scoped kubeconfig written during install so its RBAC is isolated.
- **Monitors are init-time only**: monitors are started once at daemon startup and run until the
  daemon is stopped. The HTTP API triggers *actions within* running monitors, not monitor lifecycle.
- **Supervised restart**: every monitor goroutine is wrapped in `supervisedMonitor` — crashes are
  absorbed with exponential back-off; the daemon process itself never goes down due to a single
  monitor failure.

### System Overview

```mermaid
graph TD
    systemd["systemd\nsolo-provisioner-daemon.service"] -->|start / stop| daemon

    subgraph daemon["solo-provisioner-daemon process"]
        direction TB
        cfg["daemon.yaml\n(DaemonConfig)"] -->|load & validate| run
        run["daemon.Run(ctx)"]
        run -->|errgroup| http["HTTP control plane\n(Unix socket)"]
        run -->|errgroup| sup["componentSupervisor"]
        run -->|goroutine| probe["runCompositeProbe"]

        subgraph sup["componentSupervisor"]
            direction TB
            cn_comp["component: consensus-node"]
            bn_comp["component: block-node"]
            cn_comp -->|supervised goroutine| up_mon["UpgradeMonitor"]
            cn_comp -->|supervised goroutine| mig_mon["MigrationMonitor"]
            bn_comp -->|supervised goroutine| bn_mon["blockNodeUpgradeMonitor (stub)"]
        end

        probe -->|all pass| sdnotify["sd_notify READY=1"]
    end

    http -->|"GET /health\nGET /status"| cli["solo-provisioner CLI\ndaemon service check"]
    http -->|"POST /migration/consensus/soak/start"| mig_mon
    up_mon -->|watch CRs| k8s[("Kubernetes API")]
    mig_mon -->|watch pods/metrics| k8s
```

## Binary & Entry Point

```
cmd/daemon/main.go          # entry point; loads config, applies CLI flag overrides, calls daemon.Run()
```

Persistent flags: `--config`, `--log-level`, `--version`/`-v`, `--output`/`-o`,
`--node-id`, `--orbit`, `--kubeconfig` (CN overrides applied after config load).

## Package Layout

```
internal/daemon/
├── config.go                  # DaemonConfig, DaemonComponents, typed component configs, Validate/Load/Write
├── config_v1.go               # Sealed v1 versioned structs + migrateToLatest() chain terminal
├── daemon.go                  # Daemon struct, New/NewFromConfig, Run, componentSupervisor
├── component_block_node.go    # blockNodeUpgradeMonitor stub (S7)
├── monitor.go                 # MonitorRunner interface, supervisedMonitor, StatusTracker
├── probe.go                   # ComponentProbe, ProbableMonitor, CompositeProbe
├── server.go                  # Unix-socket HTTP control plane (Server, routes)
├── handlers.go                # HTTP handler implementations
├── errors.go                  # errorx error types (ErrConfig, ErrConfigMalformed, ErrConfigNotFound)
├── types.go                   # HealthResponse, StatusResponse, ComponentStatus, MonitorState
├── sdnotify.go                # sd_notify READY=1 / STOPPING=1 integration
└── consensus/                 # Consensus-node monitor implementations
    ├── upgrade_monitor.go     # UpgradeMonitor — watches NetworkUpgradeExecute CRs
    ├── migration_monitor.go   # MigrationMonitor — soak criteria tracking
    ├── criteria.go            # SoakDuration, UploaderBacklogCleared, NoPodRestarts, ConsensusParticipationNominal
    ├── decommission.go        # Decommissioner interface + NoopDecommissioner
    └── types.go               # Shared types (OperationID, SoakState, etc.)
```

## Configuration (`daemon.yaml`)

Written by `solo-provisioner daemon service install` to `/opt/solo/weaver/config/daemon.yaml`.

```yaml
schema_version: 1
components:
  consensus_node:
    enabled: true
    kubeconfig: /opt/solo/weaver/config/daemon-cn.kubeconfig
    node_id: 0.0.3
    orbit: hedera-network
    upgrade_dir: /opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade/current
    monitors:
      upgrade: true
      migration: true
  block_node:
    enabled: true
    kubeconfig: /opt/solo/weaver/config/daemon-bn.kubeconfig
    orbit: hedera-block-node
    monitors:
      upgrade: true
```

### Schema versioning

The `schema_version` field enables forward-safe config migration:

- `LoadDaemonConfig` probes `schema_version` first, then unmarshals into the matching versioned
  struct (`daemonConfigV1`, ...), then walks a **chained single-step migration** chain via
  `migrateToLatest()` to produce the current `DaemonConfig`.
- A file missing `schema_version` (value `0`) is treated as v1 for backward compatibility.
- A file with `schema_version` newer than `CurrentSchemaVersion` is rejected immediately.
- Versioned structs in `config_vN.go` are **sealed** — never modify after shipping. To add a
  breaking change: write `config_v2.go`, add `daemonConfigV1.migrate() daemonConfigV2`, change
  `migrateToLatest()` to delegate, bump `CurrentSchemaVersion`.

## Goroutine Map

```
main.go
└── daemon.Run(ctx)
    ├── errgroup.Go → server.Start(ctx)         # Unix-socket HTTP, fatal on exit
    ├── errgroup.Go → componentSupervisor(ctx)   # never returns non-nil; absorbs all crashes
    │   ├── supervisedMonitor(ctx, UpgradeMonitor,   tracker)  # per monitor goroutine
    │   ├── supervisedMonitor(ctx, MigrationMonitor, tracker)
    │   └── supervisedMonitor(ctx, blockNodeUpgradeMonitor, tracker)  # stub
    └── go runCompositeProbe(ctx)                # fires sd_notify READY=1 when all probes pass
```

The top-level `errgroup` cancels the context if **either** `server.Start` or `componentSupervisor`
returns. Because `componentSupervisor` never returns non-nil, only a server crash will bring down
the daemon process — systemd then restarts it via `Restart=always`.

## Component Model

Each enabled component is represented by the internal `component` struct:

```go
type component struct {
    name     string
    monitors []MonitorRunner   // one goroutine per entry
    probe    ComponentProbe    // nil = immediately ready (no external deps)
    tracker  *StatusTracker    // feeds GET /status
}
```

```mermaid
graph LR
    subgraph cfg_box["DaemonConfig (daemon.yaml)"]
        cn_cfg["ConsensusNodeComponentConfig\nenabled / kubeconfig / orbit / upgrade_dir\nmonitors: upgrade=true, migration=true"]
        bn_cfg["BlockNodeComponentConfig\nenabled / kubeconfig / orbit\nmonitors: upgrade=true"]
    end

    subgraph runtime["Runtime (NewFromConfig)"]
        direction TB
        cn_comp["component: consensus-node"]
        cn_comp --- up_mon2["UpgradeMonitor"]
        cn_comp --- mig_mon2["MigrationMonitor"]
        cn_comp --- cn_probe["KubeRBACProbe + DiskProbes"]
        cn_comp --- cn_tracker["StatusTracker"]

        bn_comp["component: block-node"]
        bn_comp --- bn_mon2["blockNodeUpgradeMonitor (stub)"]
        bn_comp --- bn_probe["nil (immediately ready)"]
        bn_comp --- bn_tracker["StatusTracker"]
    end

    cn_cfg -->|"wired by NewFromConfig"| cn_comp
    bn_cfg -->|"wired by NewFromConfig"| bn_comp

    cn_tracker -->|"GET /status"| http2["HTTP /status"]
    bn_tracker -->|"GET /status"| http2
```

### Adding a new component

1. Add `FooComponentConfig` + `FooMonitors` to `config.go` and `config_v1.go` (since unreleased)
   or create `config_v2.go` with a migration step (if already released).
2. Add `FooMonitor` in `internal/daemon/component_foo.go` implementing `MonitorRunner`.
   If it needs a kubeconfig probe, also implement `ProbableMonitor`.
3. Wire it in `NewFromConfig` following the consensus-node pattern.
4. Add the component constant in `internal/ui/prompt/daemon.go` (`knownComponents` registry).
5. Wire the CLI flag and prompt step in `install.go` and `prompt/daemon.go`.

## supervisedMonitor — Back-off & Degradation

| Parameter | Default | Notes |
|---|---|---|
| Initial back-off | 5 s | First restart delay after a crash |
| Back-off multiplier | 2x | Doubles on each consecutive crash |
| Back-off cap | 5 min | Maximum delay between restarts |
| Stable threshold | 60 s | Run longer than this resets back-off and crash counter |
| Degraded threshold | 5 crashes | Emits `MonitorDegraded` error log at crash #5, #10, #15, ... |

All parameters are package-level `var`s (not `const`) so unit tests can override them without
sleeping for real durations.

### supervisedMonitor Lifecycle

```mermaid
stateDiagram-v2
    [*] --> Running : start goroutine
    Running --> Backoff : monitor.Run() returns error
    Running --> Stopped : ctx cancelled
    Running --> Running : ran > 60s (stable)\nreset backoff + crash count

    Backoff --> Running : sleep expires (5s, 10s, 20s ... cap 5min)
    Backoff --> Stopped : ctx cancelled during sleep

    Stopped --> [*]
```

> **Crash counter**: increments on every restart. Every 5th crash emits a `MonitorDegraded` error
> log. The counter and back-off delay both reset after a stable run lasting more than 60 s.

## Startup Probe

Each component can declare a `ComponentProbe` composed of one or more `Probe` leaf implementations.
The daemon runs all component probes concurrently in `runCompositeProbe`. Only when every probe
returns nil is `sd_notify READY=1` sent to systemd. If any probe fails (or ctx is cancelled by
systemd's `TimeoutStartSec`), READY is never sent and systemd marks the service failed.

Currently the consensus-node component uses a `KubeRBACProbe` that verifies the SA token can
`list` and `watch` `networkupgradeexecutes` in the orbit namespace. The block-node stub declares
no probe (`nil`) and is treated as immediately ready.

```mermaid
flowchart TD
    start(["daemon.Run starts"]) --> preflight
    preflight["kubeconfig preflight\n(build REST config for each enabled component)"] -->|fail| abort(["return error\ndaemon exits, systemd restarts"])
    preflight -->|pass| rcp

    subgraph rcp["runCompositeProbe (concurrent)"]
        direction LR
        p1["KubeRBACProbe (CN)"]
        p2["DiskOwnershipProbe (CN upgrade_dir)"]
        p3["DiskWriteTestProbe (CN upgrade_dir)"]
        p4["nil probe (BN — immediately ready)"]
    end

    rcp -->|all pass| ready["sd_notify READY=1\nsystemd marks service active"]
    rcp -->|"any fail or TimeoutStartSec expires"| noready["READY never sent\nsystemd marks service failed"]

    ready --> monitors["monitors running\nHTTP API accepting"]
```

## HTTP Control Plane

The daemon listens on a Unix socket at `/opt/solo/weaver/daemon/daemon.sock`.
All endpoints return JSON.

| Method | Path | Description |
|---|---|---|
| `GET` | `/health` | Always returns `{"status":"ok"}` while the process is alive |
| `GET` | `/status` | Per-component, per-monitor runtime state |
| `GET` | `/migration/consensus/soak/status` | Current soak state for the migration monitor |
| `POST` | `/migration/consensus/soak/start` | Enqueue a new soak run (idempotent; 409 if already active) |

The socket path is used directly by `solo-provisioner daemon service check` via `curl --unix-socket`.

## Consensus-Node Monitors

### UpgradeMonitor (`consensus/upgrade_monitor.go`)

- Watches `NetworkUpgradeExecute` CRs in the configured `orbit` namespace via the K8s watch API.
- On a new CR event, calls `handleExecute` which runs the upgrade workflow (download artefacts,
  stage to `upgrade_dir`, signal the node).
- Idempotency: a mutex-guarded `activeOpID` rejects a different `operationID` while one is in
  progress; the same `operationID` is silently re-acknowledged.
- Implements `ProbableMonitor` → `KubeRBACProbe` verifies RBAC before startup probe passes.

### MigrationMonitor (`consensus/migration_monitor.go`)

- Evaluates a set of **soak criteria** before signalling that a migration is safe to proceed.
- Criteria (all must pass): `SoakDuration` (48 h default), `UploaderBacklogCleared`,
  `NoPodRestarts`, `ConsensusParticipationNominal`.
- Idempotency: `TryEnqueue()` uses an atomic `soakActive` flag + 1-capacity channel;
  duplicate `POST /migration/consensus/soak/start` returns HTTP 409.
- Writes a structured JSONL event log to `paths.DaemonConsensusMigrateEventsDir`.

## Install Workflow

Triggered by `solo-provisioner daemon service install`. Steps run in order:

1. **Resolve config** — `--from-config`, existing `daemon.yaml`, or flags + interactive prompts.
2. **CheckClusterStep** — verify K8s API is reachable via admin kubeconfig.
3. **CreateDaemonRBACStep** — for each enabled component: idempotently create SA,
   ClusterRole, ClusterRoleBinding, and long-lived token Secret.
4. **WriteDaemonKubeconfigStep** — wait for SA token Secret, write scoped kubeconfig
   to `daemon-{cn,bn}.kubeconfig`.
5. **WriteDaemonConfigStep** — serialise `DaemonConfig` to `daemon.yaml`.
6. **InstallDaemonBinaryStep** — download (or verify local) `solo-provisioner-daemon` binary.
7. **InstallDaemonServiceStep** — write unit file to sandbox, symlink to
   `/usr/lib/systemd/system/`, `daemon-reload`, `enable`, `start`.

Uninstall runs steps 7→1 in reverse with full rollback support.

```mermaid
flowchart TD
    start(["solo-provisioner daemon service install"])
    start --> resolve["1. Resolve config\n(--from-config / existing yaml / prompts)"]
    resolve --> check["2. CheckClusterStep\nK8s API reachable?"]
    check -->|fail| err_check(["abort"])
    check -->|pass| rbac["3. CreateDaemonRBACStep\nSA + ClusterRole + ClusterRoleBinding\n+ token Secret (per component)"]
    rbac -->|fail| rollback_rbac["rollback: delete RBAC objects"]
    rbac -->|pass| kubeconfig["4. WriteDaemonKubeconfigStep\nwait for SA token, write\ndaemon-cn.kubeconfig / daemon-bn.kubeconfig"]
    kubeconfig -->|fail| rollback_kc["rollback: delete kubeconfig files"]
    kubeconfig -->|pass| write_cfg["5. WriteDaemonConfigStep\nserialise daemon.yaml"]
    write_cfg -->|fail| rollback_cfg["rollback: delete daemon.yaml"]
    write_cfg -->|pass| binary["6. InstallDaemonBinaryStep\ndownload / verify binary"]
    binary -->|fail| rollback_bin["rollback: remove binary"]
    binary -->|pass| service["7. InstallDaemonServiceStep\nwrite unit file, symlink\ndaemon-reload, enable, start"]
    service -->|fail| rollback_svc["rollback: disable & stop service\nremove unit file"]
    service -->|pass| done(["daemon running"])

    rollback_svc --> rollback_bin
    rollback_bin --> rollback_cfg
    rollback_cfg --> rollback_kc
    rollback_kc --> rollback_rbac
    rollback_rbac --> failed(["install failed (cleaned up)"])
```

## Files on Disk (production paths)

| Path | Description |
|---|---|
| `/opt/solo/weaver/config/daemon.yaml` | Main config |
| `/opt/solo/weaver/config/daemon-cn.kubeconfig` | Consensus-node scoped kubeconfig |
| `/opt/solo/weaver/config/daemon-bn.kubeconfig` | Block-node scoped kubeconfig |
| `/opt/solo/weaver/daemon/daemon.sock` | Unix socket (HTTP control plane) |
| `/opt/solo/weaver/bin/solo-provisioner-daemon` | Daemon binary (symlink target) |
| `$HOME/sandbox/usr/lib/systemd/system/solo-provisioner-daemon.service` | Unit file (sandbox) |
| `/usr/lib/systemd/system/solo-provisioner-daemon.service` | Symlink to sandbox unit |
| `/opt/solo/weaver/logs/solo-provisioner-daemon.log` | Daemon log |
| `/opt/solo/weaver/events/consensus/upgrade/` | Upgrade event JSONL files |
| `/opt/solo/weaver/events/consensus/migrate/` | Migration soak event JSONL files |

## Error Types (`errors.go`)

All errors use `joomcode/errorx`:

| Error type | When |
|---|---|
| `ErrConfig` | I/O error reading or writing config |
| `ErrConfigNotFound` | Config file does not exist |
| `ErrConfigMalformed` | YAML parse error, validation failure, or unsupported schema version |

Use `daemon.IsConfigNotFound(err)` to distinguish a missing file from a structural problem.

## Testing

```bash
# Unit tests (macOS — no Linux-only deps in daemon package)
go test -race -cover -tags='!integration' ./internal/daemon/...

# Full suite in UTM VM
task vm:test:unit
```

Key test files:
- `internal/daemon/monitor_test.go` — supervisedMonitor back-off and degradation
- `internal/daemon/server_test.go` — HTTP handler coverage
- `internal/daemon/probe_test.go` — composite probe fan-out
- `internal/daemon/consensus/upgrade_monitor_test.go` — UpgradeMonitor watch loop
- `internal/daemon/consensus/migration_monitor_test.go` — soak criteria and idempotency
- `internal/workflows/steps/step_daemon_it_test.go` — install/uninstall integration test
    (tagged `integration`; requires a running K8s cluster)

## Related Documents

- [daemon-testing-guide.md](daemon-testing-guide.md) — step-by-step human tester guide
- [migration-framework.md](migration-framework.md) — CLI startup migration framework (separate from daemon)
- [security-model.md](security-model.md) — RBAC policy rationale
- [effective-value-resolution.md](effective-value-resolution.md) — flag/config override resolution order
