# Execute-phase daemon ↔ operator contract

How the host-based `solo-provisioner-daemon` cooperates with the solo-operator
`NetworkUpgradeExecute` reconciler during the execute phase of a network upgrade
(epic #502). The contract is **HIP-1496** (network-upgrade); this note also records
where the current operator *code* deviates. `file:line` refs are into the sibling
`solo-operator` repo unless noted.

## Handshake: the daemon writes phases and conditions

Per HIP-1496 the daemon owns the two intermediate phases; the operator owns the
handoff and the terminal:

| Actor | Writes | Meaning |
|---|---|---|
| operator | `ReadyForProvisionerDaemon` | node frozen + pod scaled to zero; host-side actor takes over |
| daemon | `PendingInfraUpgrade` (only if infra needed) | infra upgrade in progress; durable checkpoint that survives cluster teardown |
| daemon | `ConfigCRsApplied=True` (condition) | all config CRs reconciled to Valid |
| daemon | `PendingNodeUpgrade` + `DaemonResult=True` (reason `Succeeded`) | success handoff; bring the CN back up |
| daemon | `DaemonResult=False` (reason) on failure — no phase advance | operator marks the CR `Failed` |
| operator | `Succeeded` / `Failed` | terminal, after reading `DaemonResult` + patching the capsule |

Phase flow: `Pending → ReadyForProvisionerDaemon → [PendingInfraUpgrade →] PendingNodeUpgrade → Succeeded | Failed`
(HIP-1496 status schema; `PendingInfraUpgrade` is skipped when no infra upgrade is
needed).

Why the daemon (not the operator) owns `PendingInfraUpgrade`/`PendingNodeUpgrade`: an
infra upgrade **tears down and reinstalls the cluster**, so the operator does not
exist during that window — only the host daemon and etcd-on-hostPath survive. The
daemon must set `PendingInfraUpgrade` before teardown (the anchor it resumes on) and
`PendingNodeUpgrade` after reinstall, without depending on the operator being alive.

Condition/reason constants (must match the operator exactly):
`api/v1alpha1/networkupgrade_common_types.go` — `DaemonResult`, `ConfigCRsApplied`,
`ReasonDaemonSucceeded="Succeeded"`, plus failure reasons
`FileDownloadFailed`/`FileHashMismatch`/`InfraUpgradeFailed`/`SelfUpgradeFailed`.

> **Operator-code deviation (must be aligned):** the current
> `networkupgradeexecute_controller.go` has the *operator* set
> `PendingInfraUpgrade` (`:288`, off `DaemonResult`) and `PendingNodeUpgrade`
> (`:319`, off `ConfigCRsApplied=True`) — i.e. the daemon writes conditions only and
> the operator derives both transitions from them. That is simpler but cannot own
> transitions across the teardown window, and it contradicts HIP-1496. Aligning on
> the HIP means the operator stops writing those two phases (reads `DaemonResult`
> only for the terminal) so it does not race the daemon.

The in-pod UC provisioner-proxy (`internal/uc/provisioner_proxy.go`) is the daemon's
**peer** for the cluster-only path and must behave identically at the handshake —
`daemon == provisioner-proxy`. Today it patches `status.phase` straight
`ReadyForProvisionerDaemon → PendingNodeUpgrade` (`:221`) and sets **no** conditions;
to align it must first set `ConfigCRsApplied=True` and `DaemonResult`, exactly as the
daemon's `reportOutcome` does, then advance the phase. It still skips
`PendingInfraUpgrade` — infra upgrades are unsupported in-pod.

## Config CRs: per-operation CRs, stable ConfigMap, audit trail

The operator delegates config-CR deployment to the daemon — only the host side can
read the upgrade package's `data/config/` (`networkupgradeexecute_controller.go:218-223`).
The daemon replicates the UC model (`internal/uc/config_cr.go`):

- For each recognised file under `<upgradePath>/data/config/` (plus package-root
  `log4j2.xml` / `settings.txt`, and per-node `block-nodes/config/block-nodes-<id>.json`),
  create a config CR of the mapped kind (11 kinds: `Log4j2Config`, `NodeSettings`,
  `ApplicationProperties`, `ApplicationOverrideProperties`, `ApiPermissionProperties`,
  `BootstrapProperties`, `NodePropertiesConfig`, `FeeSchedules`, `SimpleFeesSchedules`,
  `ThrottlesConfig`, `BlockNodesConfig`).
- CR **name** is per-operation for every kind: `<operationId>-<suffix>`
  (`config_cr.go:257`), `Create`-only (AlreadyExists → skip). Set `spec.scope =
  node<N>`, `spec.content` = file bytes, `spec.orbit`, and the
  `operator.solo.hiero.org/operation-id` label. (BlockNodesConfig previously used a
  stable name + in-place upsert; solo-operator#1321 brings it in line with the other
  kinds — per-operation, Create-only — so there is no exception.)
- CR **name is invisible to the ConsensusCapsule.** Each config CR targets a **stable**
  ConfigMap `spec.scope + "-" + suffix` (e.g. `node0-log4j2`,
  `resources/*/common.go`), and the capsule's `*ConfigRef` is that stable ConfigMap
  name — never rewired during upgrade. Node matching is by `spec.scope`.
- **Newest-CR-wins supersession** lets many CRs share one ConfigMap: the ConfigMap
  carries `operator.solo.hiero.org/owner-created-at`; a newer CR takes over
  (`ClearControllerRef` + `SetControllerReference` + overwrite `Data`,
  `resources/log4j2config/configmap.go:100-111`); older CRs mark themselves
  `Valid=Superseded` and linger as an **audit record** (never deleted,
  `common/configmap.go:85-105`). So each upgrade mints a new per-op CR that supersedes
  the prior owner and updates the shared ConfigMap.
- The daemon waits for each created CR's own `status.conditions[Valid]=True` before
  setting `ConfigCRsApplied=True`.

This differs from the CLI install path (`EnsureConfigCRs`,
`internal/workflows/steps/step_consensus_capsule.go`), which uses **stable**-named CRs
updated **in place**. Both produce the same stable ConfigMap; they differ only in CR
naming and apply policy.

## Failure semantics and termination guarantee (HIP-1496, normative)

Both provisioner paths — the host `solo-provisioner-daemon` (mainnet) and the in-pod
UC provisioner-proxy (cluster-only) — MUST present an **identical** failure contract
to the `ExecuteReconciler`. RFC-2119 keywords are normative.

- **Termination guarantee (MUST).** Every operation that enters
  `ReadyForProvisionerDaemon` MUST eventually reach a terminal phase (`Succeeded` or
  `Failed`). The provisioner MUST NOT leave an Execute CR non-terminal indefinitely.
- **Fatal failures (MUST).** Unrecoverable causes — malformed/oversized/unrecognised
  config file, `FileHashMismatch`, failed infra upgrade, or a config CR the operator
  reconciles to terminal `Failed` — MUST set `DaemonResult=False` (with `reason` +
  `message` naming the cause; for a bad config CR, the kind/name/filename) **without**
  advancing `status.phase`. MUST NOT retry a fatal failure.
- **Transient failures (SHOULD).** K8s API error, config-CR reconcile still in
  progress, network blip — SHOULD retry with bounded backoff and SHOULD NOT set
  `DaemonResult=False` while within the retry budget. Each retry SHOULD emit a warning
  event so the operation does not silently stall.
- **Deadline (MUST).** Retries MUST be bounded by a deadline **anchored to the moment
  the CR entered `ReadyForProvisionerDaemon`** and **durable across provisioner
  restarts**. On exceeding it, set `DaemonResult=False, reason=DeadlineExceeded`. The
  default deadline is configurable and implementation-defined.

On `DaemonResult=False` the `ExecuteReconciler` sets `status.phase = Failed`
(terminal); recovery is by operator intervention.

`DaemonResult` reasons (match the operator's `api/v1alpha1` exactly): `Succeeded`,
`FileHashMismatch`, `InfraUpgradeFailed`, `ConfigCRInvalid`, `DeadlineExceeded`, … .

**Daemon design consequence:** `reportOutcome` must classify the workflow error —
fatal vs transient — rather than blanket-writing `DaemonResult=False`. The operation
driver (`runExecute`) owns the retry-with-backoff loop and the durable deadline check;
only a fatal error or a blown deadline writes `DaemonResult=False`. A transient error
loops (warn event + backoff) until it clears, turns fatal, or the deadline fires.

## Idempotency

Every step — and `handleExecute` as a whole — must be idempotent. The daemon
retries (watch re-delivery, `listAndSeed` on restart, backoff) and may restart
mid-operation, so re-running must not repeat side effects:

- config CRs: create-if-absent (`AlreadyExists` is success), or upsert only when
  content differs;
- external files: verify-hash-then-skip;
- condition writes: setting a condition to the same value is a no-op (preserve
  `lastTransitionTime` when the status is unchanged).

Each step should check current state first and skip when already satisfied.

## Shared package plan

Extract the client-agnostic config-CR domain logic into a package (e.g.
`internal/operator/configcr`) consumed by both the CLI install step and the daemon
step — the daemon must not import `internal/workflows/steps` (that pulls in TUI
`notify`). The shared core: CR object builders for the 11 kinds, the
filename→kind/scope mapping, `Valid=True` waiting, and a narrow client interface
(lift `CapsuleKubeClient`). Each caller supplies naming + apply policy and its own
client adapter (install → `kube.NewClient` typed apply; daemon → a typed→unstructured
adapter over its scoped `dynamic.Interface`).

## Open items

- **RBAC:** the daemon's `daemon-cn` kubeconfig currently scopes only
  `networkupgradeexecutes`. It needs create/update/get on the 11 config-CR kinds and
  patch on `networkupgradeexecutes/status` (conditions).
- **reconfigure interaction:** the supersede model implies a future `reconfigure`
  should create a newer CR rather than update in place a CR an upgrade has superseded.
- **infra-upgrade resume:** the daemon-written `PendingInfraUpgrade` phase is the
  durable checkpoint (etcd on hostPath survives teardown); on restart the daemon
  re-reads the CR, sees `PendingInfraUpgrade`, and resumes the infra upgrade (#709).
- **operator alignment:** the operator must stop writing `PendingInfraUpgrade`/
  `PendingNodeUpgrade` (see the deviation note above) so it does not race the daemon.
