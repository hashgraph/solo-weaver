# Action history

`/opt/solo/weaver/state/action_history.yaml` records every action solo-provisioner ran on a
host and the state files each one changed. Persisted state says what a component looked like
after its last run; the action history is the only account of which command got it there.
No production code reads the file back. It is for operators and tooling.

## The contract

Every state update leaves an entry. A state update is writing, removing or moving a state file:

- a component file: `profile`, `firewall`, `system`, `cluster`, `blocknode`, `consensus`,
  `teleport` (`<id>.yaml`);
- `provisioner.yaml`;
- the pre-split legacy state: `state.yaml`, its `.legacy` and `.diverged-*` copies, and the
  `*.installed` / `*.configured` marker files.

Lock files, logs, reports and the host firewall's own config file are not state.

An action is recorded once its workflow has started, however it ends, because by then it may
have changed the host. A command that stops earlier (bad input, a lock another command holds,
state it cannot read, the drift guard) changed nothing and records nothing.

State files are written first and the entry second. A crash between the two, or an append
that fails after the state landed, leaves state with no entry. The command fails and says the
state was saved but its entry was not appended, and a re-run records its entry with no
`components`, since the files already hold its content. That is the accepted limit of writing
state and history without a transaction. When the action failed and its entry can't be appended
either, or its state was written but not synced, the command fails with the action's own error,
and a warning says what went wrong recording it. Every other way a run can end is recorded.

State files and history appends are synced to disk. Each append starts on a new line, so an
entry a full disk cut short doesn't run into the next one. A file or entry that landed but could
not be synced counts as written: its entry is not marked `notPersisted` and still names the file.
The `network firewall` verbs, the handler commands, `kube cluster install` and the
provisioner-version step say it was not synced to disk.

## Entry format

The file is a stream of YAML documents, one per entry, appended in order:

```yaml
---
intent:
  action: uninstall
  target: firewall
inputs:
  disabled: true
timestamp: 2026-10-24T09:12:03Z
outcome: succeeded
components:
  - firewall
```

| Field | Meaning |
|---|---|
| `intent` | The action and its target, e.g. `install` / `blocknode`. |
| `inputs` | The inputs the action ran with. |
| `timestamp` | When the entry was recorded. |
| `outcome` | `succeeded` or `failed`. A workflow that failed and was rolled back is `failed`: a rollback does not prove the host is back where it was. |
| `notPersisted` | Set only when a state file the action meant to write did not land, so persisted state does not match what the action did. A failed action that wrote nothing is just `failed`. |
| `error`, `reason` | Why the action failed or its state did not land, as the operator sees it, and the reason code attached to that error. |
| `components` | The state files whose content the action changed, created or removed. |
| `ownedDrift` | The out-of-band changes to the action's owned components that it ran over. |

### `components`

`components` names a file only when its content moved: a run that rewrites a file with the same
content lists nothing, and timestamps never count. So the latest entry naming a file is the
action that last changed it. A component file is named by its id; `provisioner.yaml` is
`provisioner`; the legacy files are `legacy`. When `notPersisted` is set, `components` lists
only the files that did land.

Entries written before the machine record was split into `profile`, `firewall` and `system`
name `machine`, and entries written before outcomes were recorded have no `outcome`.

### `ownedDrift`

Each record names the component and field, its persisted and live values, and:

- `destroyAllowed`: the drift guard let the run go ahead over a change its action destroys;
- `destroyed`: the run's own read after the workflow no longer finds the change;
- `unconfirmed`: the run was allowed to destroy the change, but its read after the workflow
  failed, so whether the change is gone is unknown;
- `unobserved`: the run could not read the live value at all.

## Which commands record what

| Command | Entry |
|---|---|
| `block node`, `consensus`, `teleport` commands | One entry per run, after its workflow, naming the files it changed. A run whose workflow failed still writes its state and records `failed`. |
| `network firewall create` | `install` / `firewall`. |
| `network firewall delete` (with or without `--all`) | `uninstall` / `firewall`. `delete --name` edits only the firewall's own config and records nothing. |
| `kube cluster install` | `install` / `cluster`. A failed install leaves the `system` record as it was and records `failed`. |

When a command's workflow records the provisioner version (cluster creation inside
`kube cluster install` or `block node install`), that command's entry lists `provisioner`.

Startup migrations and `kube cluster uninstall` are not recorded yet.

## Writing state in code

- Write state only through `state.Writer.Commit` (or `state.RunOwning`, for a command outside
  `BaseHandler`). It writes the listed component files, then appends the entry, on every exit:
  when a file did not land, the entry is marked `notPersisted` with the error, and an entry
  that can't be appended either is a warning, so the write error stays the one returned. When
  the entry's outcome is `failed`, a lost entry or an unsynced write is a warning too, and
  `Commit` returns nil, so the command reports the workflow's own failure.
- Use `state.Writer.Record` for an action that changed the host but whose state never reached
  `Commit`, such as a refresh that failed after the workflow. With a cause (the action failed),
  an entry that can't be appended is a warning and `Record` returns nil, so the command fails
  with the action's own error, reason and hints intact.
- Set `Outcome` from the workflow report with `steps.OutcomeOf`, and add
  `state.ChangedProvisioner` when `steps.ProvisionerVersionPersisted` says the workflow
  recorded the version.
- `TestEveryStateWriterIsAccountedFor` (`internal/state/lock_invariant_test.go`) fails when a
  call that writes state appears without a row saying how it holds its lock and how its write
  reaches the action history.
