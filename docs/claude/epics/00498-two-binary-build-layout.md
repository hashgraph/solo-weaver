# Epic #498 — Two-Binary Build Layout

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/498
> **Feature branch:** `00498-feat-two-binary-build-layout`
> **Status:** in review (all four stories landed; epic rollup PR pending)

Persistent home for cross-cutting context, design constraints, and rolling status for epic #498. Each story under this epic gets its own implementation plan under `docs/claude/plans/<branch>.md`; this document is the umbrella.

## Goal

Split the current single `solo-provisioner` binary into two compiled executables built from the same repo:

- `solo-provisioner` — one-shot CLI for provisioning + management commands.
- `solo-provisioner-daemon` — long-running, systemd-managed process that handles host-level upgrade and migration work.

## Motivation

1. **Safe upgrades.** The daemon must be replaceable independently of the CLI. Today the daemon is a subcommand inside the CLI binary, so any CLI change forces a daemon restart whether the daemon code changed or not — and the systemd unit is therefore running the entire CLI binary, including TUI and workflow engine code it never invokes.
2. **Attack-surface minimization.** Each binary should contain only the object code its features require. A long-running daemon should not bundle interactive TUI libraries; an interactive CLI should not bundle daemon event-loop plumbing.
3. **Independent release cadence.** Each binary ships through its own semantic-release pipeline (`.releaserc_cli.json` + `flow-deploy-release-cli.yaml` and `.releaserc_daemon.json` + `flow-deploy-release-daemon.yaml`) with its own tag namespace (`solo-provisioner-vX.Y.Z` and `solo-provisioner-daemon-vX.Y.Z`). The CLI and daemon can be released at different versions and on different days without coordination.

## Stories

| # | Story | Status | PR |
|---|---|---|---|
| #514 | Create `cmd/cli/main.go` entry point for `solo-provisioner` | merged | [#596](https://github.com/hashgraph/solo-weaver/pull/596) (rolled up via [#597](https://github.com/hashgraph/solo-weaver/pull/597)) |
| #515 | Create `cmd/daemon/main.go` entry point for `solo-provisioner-daemon` (own Cobra root) | merged | [#596](https://github.com/hashgraph/solo-weaver/pull/596) (bundled with #514; rolled up via [#597](https://github.com/hashgraph/solo-weaver/pull/597)) |
| #516 | Update `Taskfile.yaml` so build targets produce both binaries per platform | in review | this PR (bundled with #517) |
| #517 | Update CI/CD to publish both binaries as independent release processes | in review | this PR (bundled with #516) |

## Design constraints

Setting the design contract for the epic so individual story plans don't drift from it:

1. **Two binaries, not one — required for safe upgrades.** The daemon must be replaceable independently of the CLI.
2. **Each binary includes only the object code needed for its own features.** The daemon binary must not bundle CLI-only packages, and vice versa. Enforced via `go list -deps` checks per binary.
3. **Layout is symmetric: `cmd/cli/main.go` and `cmd/daemon/main.go`.** Same repo, same code base, two compiled executables.
4. **Independent build and release per binary.** Each binary gets its own Taskfile target family (`build:cli:*` and `build:daemon:*`) and its own end-to-end release pipeline (`.releaserc_cli.json` / `flow-deploy-release-cli.yaml` and `.releaserc_daemon.json` / `flow-deploy-release-daemon.yaml`). Tag namespaces are distinct (`solo-provisioner-vX.Y.Z` and `solo-provisioner-daemon-vX.Y.Z`); VERSION files are per-binary at `pkg/version/{cli,daemon}/VERSION`. Each workflow is `workflow_dispatch`-only and can be triggered at any cadence — there is no requirement for the two binaries to share a release tag or a release time.
5. **Daemon is a long-running process; CLI is one-shot.** The daemon survives across CLI invocations; the CLI talks to it (or merely co-exists with it) at runtime. The specific runtime contract between them — IPC mechanism, message format, compatibility/protocol versioning — is a future concern, out of scope for this epic.

## Branching convention

This epic uses the team's multi-story feature-branch convention:

```
main
└── 00498-feat-two-binary-build-layout       (long-lived epic feature branch)
    ├── 00514-cli-main-entry-point             (story PR → targets epic branch)
    ├── 00515-...                               (future story PR)
    ├── 00516-...                               (future story PR)
    └── 00517-...                               (future story PR)
```

Stories merge into `00498-feat-two-binary-build-layout` incrementally; once all stories are landed, the epic rollup PR (this PR) merges the cumulative diff into `main`. Intermediate states (e.g. the daemon binary exists but the release pipeline doesn't ship it yet) live on the epic branch and never appear on `main`.

## Future work (outside this epic)

- **CLI ↔ daemon runtime contract.** IPC mechanism, message format, protocol versioning. Likely a follow-on epic.
- **Self-upgrade hooks.** The CLI's `solo-provisioner self-upgrade` (or equivalent) should compare protocol versions before bouncing the daemon service.
- **Scope-filtered release rules.** Today both pipelines compute the next version off every commit since the last tag in their namespace. If cross-binary commit traffic causes spurious version bumps, add `releaseRules` scope filters in each `.releaserc_*.json`.

## References

- Sibling story plans (under `docs/claude/plans/`):
  - `00514-cli-main-entry-point.md` — #514 + #515 combined plan.
  - `00516-taskfile-daemon-build-targets.md` — #516 + #517 combined plan.
- HIP context (background, not load-bearing for the epic):
  - `hip-xxxx1 - network-deployment.md` — `solo-provisioner` CLI vs daemon roles.
  - `solo-weaver-catalog-alternatives.md` — embedded catalog versioning model.
