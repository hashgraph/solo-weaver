# Functionality Test Suite — State Model Branch

> **Branch:** `00370-state-data-model`
> **Date:** 2026-03-13
> **Purpose:** Verify that all existing functionality is preserved after the introduction of the new unified state model, and that migration from old state to new is seamless.

---

## Legend

| Symbol | Meaning |
|--------|---------|
| 🔄 | Migration / Backward compatibility |
| 🧱 | Block Node command |
| ☸️ | Kubernetes Cluster command |
| 🛡️ | Teleport command |
| 🔍 | Reality checker / State refresh |
| 💾 | State persistence / Flush |
| 🧮 | Effective value resolution (RSL) |
| 🧩 | Model / Data structure |
| 🔧 | Software installer |
| 🏗️ | BLL / Intent handler |

---

## 1. State Migration (🔄)

### 1.1 Legacy-to-Unified Migration (`UnifiedStateMigration`)

- [ ] **TC-MIG-001** — As a node operator, when I run the new version with legacy `*.installed` and `*.configured` state files on disk, they migrate seamlessly into the new `state.yaml` unified state model (`MachineState.Software` entries).
- [ ] **TC-MIG-002** — After migration, the legacy `*.installed` / `*.configured` files are removed from the state directory.
- [ ] **TC-MIG-003** — If no legacy state files exist, `Applies()` returns `false` and no migration runs.
- [ ] **TC-MIG-004** — If the state directory does not exist, `Applies()` returns `false` gracefully.
- [ ] **TC-MIG-005** — `Rollback()` restores the legacy `*.installed` / `*.configured` files from the unified state.
- [ ] **TC-MIG-006** — A round-trip test: migrate forward, rollback, migrate again — the final state must match the first migration result.
- [ ] **TC-MIG-007** — Legacy state files with both `installed` and `configured` entries for the same component (e.g. `cilium.installed` + `cilium.configured`) produce a single `SoftwareState` with `Installed=true` and `Configured=true`.
- [ ] **TC-MIG-008** — The `version` string is correctly parsed from legacy content format `"installed at version X.Y.Z"`.
- [ ] **TC-MIG-009** — Migrations run in sequential order as registered in `InitMigrations()`.

### 1.2 State File Persistence & Integrity

- [ ] **TC-STA-001** — `NewStateManager()` creates a manager with default state (`ModelVersion = "v1"`, provisioner info populated).
- [ ] **TC-STA-002** — `FlushState()` writes the state file atomically (temp file + rename).
- [ ] **TC-STA-003** — `FlushState()` computes a SHA-256 hash and stores it in `state.Hash` / `state.HashAlgo`.
- [ ] **TC-STA-004** — `FlushState()` detects external concurrent modifications and refuses to overwrite (optimistic concurrency via hash comparison).
- [ ] **TC-STA-005** — `FlushState()` is a no-op (no disk write) if the state content has not changed (hash comparison against baseline).
- [ ] **TC-STA-006** — `Refresh()` reloads state from disk and replaces the in-memory state correctly.
- [ ] **TC-STA-007** — `Refresh()` returns `NotFoundError` when the state file does not exist (first-run scenario).
- [ ] **TC-STA-008** — `Set()` + `FlushAll()` persists both state and action history.
- [ ] **TC-STA-009** — `AddActionHistory()` appends entries and updates `State.LastAction`; entries are written to `action_history.yaml` on `FlushActionHistory()`.
- [ ] **TC-STA-010** — `HasPersistedState()` reports correctly whether the state file exists on disk.

---

## 2. Block Node Commands (🧱)

### 2.1 Block Node Install

- [ ] **TC-BN-INS-001** — As a node operator, when I run `block node install` on a fresh machine, the cluster is bootstrapped and the block node is installed.
- [ ] **TC-BN-INS-002** — As a node operator, when I run `block node install` and a block node is already deployed (`status=deployed`), the command errors with a message to use `upgrade` or `reset` (or pass `--force`).
- [ ] **TC-BN-INS-003** — When the cluster already exists (`ClusterState.Created = true`), the install skips the cluster bootstrap step and installs only the block node.
- [ ] **TC-BN-INS-004** — `--force` flag bypasses the "already installed" guard and re-runs the install workflow.
- [ ] **TC-BN-INS-005** — After a successful install, the state is flushed to disk with `BlockNodeState.ReleaseInfo.Status = deployed`.
- [ ] **TC-BN-INS-006** — After a successful install, `ActionHistory` is persisted with `intent.action = install`, `intent.target = blocknode`.
- [ ] **TC-BN-INS-007** — The `injectChartRef()` callback correctly writes `ChartRef` into `BlockNodeState.ReleaseInfo.ChartRef` after workflow execution.
- [ ] **TC-BN-INS-008** — Storage `basePath` is resolved from config when not provided by the user.

### 2.2 Block Node Upgrade

- [ ] **TC-BN-UPG-001** — As a node operator, when I run `block node upgrade` and the block node is deployed, it upgrades successfully.
- [ ] **TC-BN-UPG-002** — `block node upgrade` errors if the block node is not installed (unless `--force`).
- [ ] **TC-BN-UPG-003** — `block node upgrade` errors if the chart reference is changed (chart immutability enforced).
- [ ] **TC-BN-UPG-004** — `block node upgrade` errors if the target version is older than the current version (downgrade prevention via semver).
- [ ] **TC-BN-UPG-005** — `block node upgrade` errors if the target version equals the current version (unless `--force`).
- [ ] **TC-BN-UPG-006** — `block node upgrade --with-storage-reset` includes the `PurgeBlockNodeStorage` step before the upgrade step.
- [ ] **TC-BN-UPG-007** — After a successful upgrade, the state is flushed with the new version and `ChartRef` is preserved via the callback.

### 2.3 Block Node Reset

- [ ] **TC-BN-RST-001** — As a node operator, when I run `block node reset`, the block node is reset (scaled down, storage cleared, scaled up).
- [ ] **TC-BN-RST-002** — `block node reset` errors if the block node is not installed (unless `--force`).
- [ ] **TC-BN-RST-003** — After a successful reset, state is flushed to disk correctly.

### 2.4 Block Node Uninstall

- [ ] **TC-BN-UNI-001** — As a node operator, when I run `block node uninstall`, the Helm release is removed.
- [ ] **TC-BN-UNI-002** — `block node uninstall` errors if the block node is not installed (unless `--force`).
- [ ] **TC-BN-UNI-003** — `block node uninstall --with-storage-reset` includes the `PurgeBlockNodeStorage` step.
- [ ] **TC-BN-UNI-004** — After a successful uninstall, state is flushed and `BlockNodeState.ReleaseInfo.Status` is no longer `deployed`.

---

## 3. Kubernetes Cluster Commands (☸️)

### 3.1 Cluster Install

- [ ] **TC-CL-INS-001** — As a node operator, when I run `kube cluster install`, the cluster is set up with the specified profile and node type.
- [ ] **TC-CL-INS-002** — As a node operator, when I run `kube cluster install` and the cluster is already created (`ClusterState.Created = true`), the command should detect the existing setup and error if it is already installed.
- [ ] **TC-CL-INS-003** — Invalid profile or node type is rejected with a clear error message.
- [ ] **TC-CL-INS-004** — State manager is initialised, refreshed, and passed into the workflow correctly.

### 3.2 Cluster Uninstall

- [ ] **TC-CL-UNI-001** — As a node operator, when I run `kube cluster uninstall`, the existing cluster setup is detected and uninstalled if required.
- [ ] **TC-CL-UNI-002** — Uninstall correctly handles bind mount cleanup and resource teardown.

---

## 4. Teleport Commands (🛡️)

### 4.1 Teleport Cluster Install

- [ ] **TC-TP-CI-001** — As a node operator, when I run `teleport cluster install`, it detects the existing setup and errors if it is already installed.
- [ ] **TC-TP-CI-002** — `teleport cluster install` requires `--values` flag; missing flag returns an error.
- [ ] **TC-TP-CI-003** — Values file path is validated for security (path traversal, symlinks).

### 4.2 Teleport Cluster Uninstall

- [ ] **TC-TP-CU-001** — As a node operator, when I run `teleport cluster uninstall`, it detects the existing setup and uninstalls if required.

### 4.3 Teleport Node Install

- [ ] **TC-TP-NI-001** — As a node operator, when I run `teleport node install`, it detects the existing setup and errors if it is already installed.
- [ ] **TC-TP-NI-002** — `teleport node install` requires `--token` flag; missing flag returns an error.
- [ ] **TC-TP-NI-003** — State manager is initialised and refreshed correctly before the workflow runs.

### 4.4 Teleport Node Uninstall

- [ ] **TC-TP-NU-001** — As a node operator, when I run `teleport node uninstall`, it detects the existing setup and uninstalls if required.
- [ ] **TC-TP-NU-002** — When the node agent is not installed, `teleport node uninstall` skips gracefully without error.
- [ ] **TC-TP-NU-003** — After uninstall, TeleportState is persisted to disk with `nodeAgent.installed: false`.

---

## 5. Effective Value Resolution — RSL (🧮)

### 5.1 BlockNodeRuntimeResolver

- [ ] **TC-RSL-BN-001** — When a block node release is deployed, all resolver methods (`Namespace()`, `ReleaseName()`, `ChartRef()`, `ChartName()`, `ChartVersion()`, `Version()`, `Storage()`) return the current state value with `StrategyCurrent`.
- [ ] **TC-RSL-BN-002** — When a block node release is deployed and a required field (e.g. `Namespace`) is empty, an `IllegalState` error is returned.
- [ ] **TC-RSL-BN-003** — When no release is deployed and user inputs are provided, resolver methods return user input values with `StrategyUserInput`.
- [ ] **TC-RSL-BN-004** — When neither current state nor user inputs supply a value, resolver methods return config defaults with `StrategyConfig`.
- [ ] **TC-RSL-BN-005** — `ChartRef()` logs a warning (but doesn't error) when a deployed release has empty `ChartRef` and falls back correctly.
- [ ] **TC-RSL-BN-006** — `resolveBlocknodeEffectiveInputs()` produces a fully resolved `UserInputs[BlockNodeInputs]` for all block node commands.
- [ ] **TC-RSL-BN-007** — `WithUserInputs()`, `WithConfig()`, `WithState()` correctly set resolver sources.

### 5.2 ClusterRuntimeResolver

- [ ] **TC-RSL-CL-001** — `RefreshState()` skips refresh when `LastSync` is within the refresh interval (staleness check).
- [ ] **TC-RSL-CL-002** — `RefreshState(ctx, true)` always refreshes regardless of the refresh interval.
- [ ] **TC-RSL-CL-003** — `CurrentState()` returns a copy (not a pointer to the internal state).

### 5.3 BlockNodeRuntimeResolver — RefreshState

- [ ] **TC-RSL-BNRS-001** — `RefreshState()` respects refresh interval via direct `LastSync` check (not helper functions).
- [ ] **TC-RSL-BNRS-002** — `RefreshState()` preserves `ChartRef` from old state if the reality checker returns empty `ChartRef` but old state had one (deployed release).
- [ ] **TC-RSL-BNRS-003** — `RefreshState()` clones old state before refresh so original is not mutated.

### 5.4 MachineRuntimeResolver — RefreshState

- [ ] **TC-RSL-MR-001** — `RefreshState()` skips refresh when `LastSync` is within the refresh interval.
- [ ] **TC-RSL-MR-002** — `RefreshState(ctx, true)` always refreshes.
- [ ] **TC-RSL-MR-003** — `RefreshState()` replaces internal state under lock.

### 5.5 RuntimeResolver (Composite)

- [ ] **TC-RSL-RT-001** — `Refresh()` calls `RefreshState()` on all three runtimes (cluster, block node, machine).
- [ ] **TC-RSL-RT-002** — `CurrentState()` composes a single `state.State` from all three runtime states.
- [ ] **TC-RSL-RT-003** — `FlushAll()` persists the composed state to disk.

---

## 6. Reality Checkers (🔍)

### 6.1 ClusterChecker

- [ ] **TC-RC-CL-001** — `RefreshState()` returns an empty `ClusterState` when the cluster does not exist.
- [ ] **TC-RC-CL-002** — `RefreshState()` populates `ClusterInfo` and sets `Created = true` when the cluster is reachable.
- [ ] **TC-RC-CL-003** — `FlushState()` skips writes when the state is unchanged (comparator-based).
- [ ] **TC-RC-CL-004** — `FlushState()` refreshes from disk before writing to avoid overwriting concurrent updates.
- [ ] **TC-RC-CL-005** — State persisted to disk matches the in-memory value (nil vs empty map handling for `Clusters`/`Contexts`).

### 6.2 MachineChecker

- [ ] **TC-RC-MC-001** — `RefreshState()` returns populated `SoftwareState` entries for all registered software installers.
- [ ] **TC-RC-MC-002** — `RefreshState()` returns populated `HardwareState` entries (OS, CPU, memory, storage).
- [ ] **TC-RC-MC-003** — `FlushState()` skips writes when the machine state is unchanged.
- [ ] **TC-RC-MC-004** — State persisted to disk matches the in-memory value after refresh.
- [ ] **TC-RC-MC-005** — Software installers that fail verification log errors but do not prevent other software from being checked.

### 6.3 BlockNodeChecker

- [ ] **TC-RC-BN-001** — `RefreshState()` returns default `BlockNodeState` when no cluster exists.
- [ ] **TC-RC-BN-002** — `RefreshState()` finds the block node Helm release by scanning for StatefulSet with `app.kubernetes.io/instance=block-node`.
- [ ] **TC-RC-BN-003** — `RefreshState()` populates `HelmReleaseInfo` correctly from the Helm release object.
- [ ] **TC-RC-BN-004** — `RefreshState()` populates storage paths/sizes from PersistentVolumes (live, archive, log, verification).
- [ ] **TC-RC-BN-005** — `ChartRef` is intentionally left empty in `RefreshState()` (not stored in Helm; injected by caller).
- [ ] **TC-RC-BN-006** — When a previously deployed release is now missing, state resets to `NewBlockNodeState()`.
- [ ] **TC-RC-BN-007** — `FlushState()` skips writes when `BlockNodeState` is unchanged.

---

## 7. State Data Model (🧩)

### 7.1 State Struct

- [ ] **TC-MDL-001** — `NewState()` creates a state with `Version = "v1"`, provisioner info, and empty sub-states.
- [ ] **TC-MDL-002** — `NewMachineState()` creates a machine state with initialized (non-nil) Software and Hardware maps.
- [ ] **TC-MDL-003** — `NewClusterState()` creates a cluster state with `Created = false`.
- [ ] **TC-MDL-004** — `NewBlockNodeState()` creates a block node state with `ReleaseInfo.Status = StatusUnknown`.

### 7.2 Cloner

- [ ] **TC-CLN-001** — `State.Clone()` produces a deep copy; mutating the clone does not affect the original.
- [ ] **TC-CLN-002** — `MachineState.Clone()` deep-copies Software and Hardware maps.
- [ ] **TC-CLN-003** — `SoftwareState.Clone()` deep-copies Metadata (`StringMap`).
- [ ] **TC-CLN-004** — `ClusterNodeState.Clone()` deep-copies Labels and Annotations maps.
- [ ] **TC-CLN-005** — `BlockNodeState.Clone()` produces a copy.
- [ ] **TC-CLN-006** — `ClusterState.Clone()` produces a copy.
- [ ] **TC-CLN-007** — `HelmReleaseInfo.Clone()` produces a copy.

### 7.3 Comparator

- [ ] **TC-CMP-001** — `SoftwareState.Equal()` returns `true` for identical values (ignoring `LastSync`).
- [ ] **TC-CMP-002** — `SoftwareState.Equal()` returns `false` when any field differs.
- [ ] **TC-CMP-003** — `HardwareState.Equal()` ignores `LastSync` but compares all other fields.
- [ ] **TC-CMP-004** — `MachineState.Equal()` compares all software and hardware entries.
- [ ] **TC-CMP-005** — `ClusterState.Equal()` compares `Created` flag and `ClusterInfo`.
- [ ] **TC-CMP-006** — `ClusterInfo.Equal()` treats nil and empty maps as equivalent for `Clusters`/`Contexts`.
- [ ] **TC-CMP-007** — `BlockNodeState.Equal()` compares `ReleaseInfo` and `Storage`.
- [ ] **TC-CMP-008** — `HelmReleaseInfo.Equal()` compares all fields except time fields.
- [ ] **TC-CMP-009** — `ClusterNodeState.Equal()` compares Labels and Annotations maps.

### 7.4 StringMap

- [ ] **TC-SM-001** — `NewStringMap()` returns an initialized empty map.
- [ ] **TC-SM-002** — `Get()`, `Set()`, `Delete()` work correctly.
- [ ] **TC-SM-003** — `Clone()` produces a deep copy; mutating clone does not affect original.
- [ ] **TC-SM-004** — `IsEqual()` returns `true` for identical maps, `true` for both nil, `false` for nil vs non-nil.
- [ ] **TC-SM-005** — `Merge()` correctly adds/overwrites entries from another map.
- [ ] **TC-SM-006** — `Keys()` and `Values()` return all keys/values.
- [ ] **TC-SM-007** — YAML marshal/unmarshal round-trip produces equivalent `StringMap`.

### 7.5 SoftwareState Helpers

- [ ] **TC-SSH-001** — `GetSoftwareState()` returns zero-value `SoftwareState` (with `Name` set) when the component is absent.
- [ ] **TC-SSH-002** — `SetSoftwareState()` adds a new entry and sets `LastSync`.
- [ ] **TC-SSH-003** — `SetSoftwareState()` initializes the `Software` map if it is nil.

---

## 8. BLL — Intent Handlers (🏗️)

### 8.1 BaseHandler

- [ ] **TC-BLL-BH-001** — `NewBaseHandler()` errors when `RuntimeResolver` is nil.
- [ ] **TC-BLL-BH-002** — `ValidateIntent()` rejects invalid intents (empty action, wrong target).
- [ ] **TC-BLL-BH-003** — `ValidateIntent()` rejects invalid user inputs (bad profile, bad namespace, etc.).
- [ ] **TC-BLL-BH-004** — `HandleIntent()` orchestrates: validate → refresh → prepare effective inputs → build workflow → execute → flush.
- [ ] **TC-BLL-BH-005** — `FlushState()` calls `Refresh(ctx, true)` to get latest state, applies callback, and flushes.
- [ ] **TC-BLL-BH-006** — `FlushState()` writes action history entry before flushing.

### 8.2 Block Node HandlerRegistry

- [ ] **TC-BLL-BNH-001** — `NewHandlerFactory()` errors when `state.Manager` is nil.
- [ ] **TC-BLL-BNH-002** — `NewHandlerFactory()` errors when `RuntimeResolver` is nil.
- [ ] **TC-BLL-BNH-003** — `NewHandlerFactory()` errors when `BlockNodeRuntime` is nil or wrong type.
- [ ] **TC-BLL-BNH-004** — `ForAction(ActionInstall)` returns `InstallHandler`.
- [ ] **TC-BLL-BNH-005** — `ForAction(ActionUpgrade)` returns `UpgradeHandler`.
- [ ] **TC-BLL-BNH-006** — `ForAction(ActionReset)` returns `ResetHandler`.
- [ ] **TC-BLL-BNH-007** — `ForAction(ActionUninstall)` returns `UninstallHandler`.
- [ ] **TC-BLL-BNH-008** — `ForAction()` returns an error for unsupported action types.

### 8.3 Cluster HandlerFactory

- [ ] **TC-BLL-CLH-001** — `NewHandlerFactory()` creates a factory with an install handler.
- [ ] **TC-BLL-CLH-002** — `ForAction(ActionInstall)` returns the install handler.
- [ ] **TC-BLL-CLH-003** — `ForAction()` returns an error for unsupported actions (upgrade, uninstall not yet implemented).

### 8.4 ChartRef Injection Callback

- [ ] **TC-BLL-CR-001** — `injectChartRef()` sets `ChartRef` in `BlockNodeState.ReleaseInfo` when the release is deployed and user provided a chart reference.
- [ ] **TC-BLL-CR-002** — `injectChartRef()` is a no-op when the release is not deployed.
- [ ] **TC-BLL-CR-003** — `injectChartRef()` is a no-op when the user did not provide a chart reference.
- [ ] **TC-BLL-CR-004** — `injectChartRef()` also injects storage `basePath` if provided.

---

## 9. Models & Validation (🧩)

### 9.1 Intent

- [ ] **TC-INT-001** — `Intent.IsValid()` returns `true` for all allowed action-target combinations.
- [ ] **TC-INT-002** — `Intent.IsValid()` returns `false` for empty action or target.
- [ ] **TC-INT-003** — `Intent.IsValid()` returns `false` for unrecognized action-target combinations.

### 9.2 UserInputs

- [ ] **TC-INP-001** — `UserInputs[BlockNodeInputs].Validate()` passes for valid inputs.
- [ ] **TC-INP-002** — `UserInputs[BlockNodeInputs].Validate()` rejects invalid profile names.
- [ ] **TC-INP-003** — `UserInputs[BlockNodeInputs].Validate()` rejects invalid namespace / release / chart identifiers.
- [ ] **TC-INP-004** — `UserInputs[BlockNodeInputs].Validate()` calls `BlockNodeStorage.Validate()`.
- [ ] **TC-INP-005** — `UserInputs[ClusterInputs].Validate()` passes for valid inputs.
- [ ] **TC-INP-006** — `UserInputs[MachineInputs].Validate()` passes for valid inputs.

### 9.3 BlockNodeStorage Validation

- [ ] **TC-STO-001** — `BlockNodeStorage.Validate()` passes when `basePath` is provided.
- [ ] **TC-STO-002** — `BlockNodeStorage.Validate()` passes when all individual paths (`archivePath`, `livePath`, `logPath`, `verificationPath`) are provided.
- [ ] **TC-STO-003** — `BlockNodeStorage.Validate()` errors when neither `basePath` nor all individual paths are provided.
- [ ] **TC-STO-004** — Zero-value `BlockNodeStorage` validation behavior is consistent with the rule (either basePath or all paths).

### 9.4 Config

- [ ] **TC-CFG-001** — `config.Get()` loads and returns a valid `Config` from the config file.
- [ ] **TC-CFG-002** — `Config.Validate()` passes for the test `config.yaml`.
- [ ] **TC-CFG-003** — `OverrideBlockNodeConfig()` correctly applies flag overrides to config values.

---

## 10. Software Installers (🔧)

### 10.1 VerifyInstallation

- [ ] **TC-SW-VI-001** — `VerifyInstallation()` returns a populated `SoftwareState` with `Installed=true`, `Configured=true` when software is correctly installed and configured.
- [ ] **TC-SW-VI-002** — `VerifyInstallation()` returns `Installed=false` when sandbox binaries are missing.
- [ ] **TC-SW-VI-003** — `VerifyInstallation()` returns `Configured=false` when sandbox configs are missing or incomplete.
- [ ] **TC-SW-VI-004** — `verifySandboxBinaries()` checks for binary file existence in sandbox.
- [ ] **TC-SW-VI-005** — `verifySandboxConfigs()` is overloaded per installer (kubelet checks patches + symlinks; cilium checks config files).

### 10.2 Kubelet Installer

- [ ] **TC-SW-KUB-001** — `kubeletInstaller.verifySandboxConfigs()` checks config file existence.
- [ ] **TC-SW-KUB-002** — `kubeletInstaller.verifySandboxConfigs()` verifies service file patching.
- [ ] **TC-SW-KUB-003** — `kubeletInstaller.verifySandboxConfigs()` verifies symlink existence.
- [ ] **TC-SW-KUB-004** — `kubeletInstaller.verifySandboxConfigs()` returns metadata including `kubeletServicePath`.

### 10.3 Cilium Installer

- [ ] **TC-SW-CIL-001** — `ciliumInstaller.verifySandboxConfigs()` checks for cilium config files.

### 10.4 Other Installers

- [ ] **TC-SW-OTH-001** — CRI-O: `VerifyInstallation()` returns correct state.
- [ ] **TC-SW-OTH-002** — Helm: `VerifyInstallation()` returns correct state.
- [ ] **TC-SW-OTH-003** — Kubeadm: `VerifyInstallation()` returns correct state.
- [ ] **TC-SW-OTH-004** — Kubectl: `VerifyInstallation()` returns correct state.
- [ ] **TC-SW-OTH-005** — K9s: `VerifyInstallation()` returns correct state.
- [ ] **TC-SW-OTH-006** — Teleport: `VerifyInstallation()` returns correct state.

---

## 11. Workflow Steps (Integration)

### 11.1 Software Setup Steps

- [ ] **TC-WF-SW-001** — Each software step (cilium, cri-o, helm, kubeadm, kubectl, kubelet, k9s, teleport) correctly receives the state manager.
- [ ] **TC-WF-SW-002** — Steps use the `Software` interface and do not depend on legacy state file paths.

### 11.2 Block Node Steps

- [ ] **TC-WF-BN-001** — `SetupBlockNode()` step installs the block node Helm chart with resolved inputs.
- [ ] **TC-WF-BN-002** — `UpgradeBlockNode()` step upgrades the existing Helm release.
- [ ] **TC-WF-BN-003** — `ResetBlockNode()` step scales down, purges storage, and scales back up.
- [ ] **TC-WF-BN-004** — `UninstallBlockNode()` step removes the Helm release.
- [ ] **TC-WF-BN-005** — `PurgeBlockNodeStorage()` step clears all storage directories.

### 11.3 Cluster Steps

- [ ] **TC-WF-CL-001** — `InstallClusterWorkflow()` includes all required setup steps (directories, sysctl, software, bind mounts, systemd, kubeadm init).
- [ ] **TC-WF-CL-002** — Cluster uninstall workflow cleans up bind mounts, kubeadm reset, and related resources.

---

## 12. Alloy Commands

### 12.1 Alloy Cluster Install

- [ ] **TC-AL-CI-001** — As a node operator, when I run `alloy cluster install`, it detects the existing setup and errors if it is already installed.

### 12.2 Alloy Cluster Uninstall

- [ ] **TC-AL-CU-001** — As a node operator, when I run `alloy cluster uninstall`, it detects the existing setup and uninstalls if required.

---

## 13. Kube Client & Admin (Integration)

- [ ] **TC-KB-001** — `kube.NewClient()` creates a client that can list resources (PV, StatefulSet, etc.).
- [ ] **TC-KB-002** — `kube.ClusterExists()` correctly detects whether a cluster is reachable (based on kubeconfig existence).
- [ ] **TC-KB-003** — `kube.RetrieveClusterInfo()` returns populated `ClusterInfo` with server version, host, clusters, contexts.
- [ ] **TC-KB-004** — `kube.Admin` operations (create namespace, apply manifests) function correctly.

---

## 14. End-to-End Scenarios

### 14.1 Fresh Install E2E

- [ ] **TC-E2E-001** — On a clean machine: `block node install --profile local` bootstraps the cluster, installs the block node, and persists state to `state.yaml`.
- [ ] **TC-E2E-002** — State file contains correct `BlockNodeState`, `ClusterState`, `MachineState`, `ProvisionerInfo`, and `ActionHistory`.

### 14.2 Upgrade E2E

- [ ] **TC-E2E-003** — After install: `block node upgrade --version X.Y.Z` upgrades the release and persists updated state.
- [ ] **TC-E2E-004** — Upgrade preserves `ChartRef` across refresh cycles.

### 14.3 Reset E2E

- [ ] **TC-E2E-005** — After install: `block node reset` resets the node and persists state.

### 14.4 Uninstall E2E

- [ ] **TC-E2E-006** — After install: `block node uninstall` removes the release and persists state.

### 14.5 Migration E2E

- [ ] **TC-E2E-007** — A machine running the old binary with legacy state files is upgraded to the new binary; state migrates seamlessly on first command execution.

---

## 15. Error Handling & Edge Cases

- [ ] **TC-ERR-001** — All commands return clear error messages with resolution hints (via `ErrPropertyResolution`).
- [ ] **TC-ERR-002** — `--force` flag overrides all precondition guards (install when deployed, upgrade when not deployed, etc.).
- [ ] **TC-ERR-003** — YAML marshal/unmarshal of `StringMap` (replacing `automa.StateBag`) works correctly for state persistence.
- [ ] **TC-ERR-004** — Concurrent state modifications by different checkers are handled via refresh-before-write pattern in `FlushState()`.
- [ ] **TC-ERR-005** — `BlockNodeStorage.Validate()` correctly enforces "either basePath or all of archivePath, livePath, logPath, verificationPath".
- [ ] **TC-ERR-006** — Helm template rendering errors (e.g. missing `loadBalancer.enabled`) produce clear diagnostic messages.

---

## Test File Reference

The following test files may need review or updates to align with the new state model:

| File | Status | Notes |
|------|--------|-------|
| `internal/state/migration_unified_state_test.go` | ⚠️ Updated | Uses `NewState()`, `GetSoftwareState()`, `SetSoftwareState()` |
| `internal/state/state_manager_test.go` | ❌ Deleted | Old `Manager` tests removed; need new `stateManager` tests |
| `pkg/models/validation_test.go` | ⚠️ Failing | `BlockNodeStorage.Validate()` rule change breaks some cases |
| `pkg/models/inputs_test.go` | ⚠️ Failing | Zero-value `BlockNodeInputs` validation affected |
| `pkg/models/string_map_test.go` | ✅ New | Tests for `StringMap` (replacement for `automa.StateBag`) |
| `internal/reality/block_node_it_test.go` | ✅ New | Integration test for block node checker |
| `internal/reality/cluster_checker_it_test.go` | ✅ New | Integration test for cluster checker |
| `internal/reality/machine_checker_it_test.go` | ✅ New | Integration test for machine checker |
| `pkg/software/base_installer_test.go` | ⚠️ Updated | Tests for `VerifyInstallation()` |
| `pkg/software/kubelet_installer_it_test.go` | ⚠️ Updated | Tests for kubelet sandbox config verification |
| `internal/workflows/steps/step_block_node_test.go` | ⚠️ Updated | Step tests adjusted for new input types |
| `internal/workflows/steps/helpers_test.go` | ⚠️ Updated | Helper test adjustments |
| `cmd/weaver/commands/block/node/install_upgrade_it_test.go` | ⚠️ Updated | Integration test for install/upgrade flow |

