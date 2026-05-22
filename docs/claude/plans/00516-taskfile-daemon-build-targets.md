# #516 + #517 — Taskfile aggregator wiring + independent release processes for the two binaries

> **Issues:** https://github.com/hashgraph/solo-weaver/issues/516 and https://github.com/hashgraph/solo-weaver/issues/517
> **Epic:** #498 — Two-Binary Build Layout
> **Story branch:** `00516-taskfile-daemon-build-targets`
> **PR base:** `00498-feat-two-binary-build-layout` (re-pushed off `origin/main` @ `b294657` after the prior epic rollup #597 merged)
> **PR closes:** #516, #517

## Summary

#514 and #515 (PR #596, rolled up via #597) landed `cmd/cli/main.go`, `cmd/daemon/main.go`, and the per-binary Taskfile families (`build:cli:*`, `build:daemon:*`, etc.). This PR finishes epic #498:

- **#516 — top-level Taskfile aggregator wiring.** `task build` / `task hash` / `task sign` now invoke both `:cli:all` and `:daemon:all` so a single dev command produces both binaries. The per-binary aggregators (`hash:cli:all`, `sign:cli:all`, and daemon equivalents) gain `deps: [build:*:all]` so they can stand alone without the top-level wrapper — the release pipelines below rely on this.
- **#517 — independent release processes.** Replace the single `.releaserc` + single release workflow with two independent semantic-release pipelines, one per binary. Each can be triggered independently and at a different cadence; each owns its own version file, its own tag namespace, and its own set of release assets.

Sibling-story scope (verbatim from issue bodies):

| # | Story | Body excerpt | Status |
|---|---|---|---|
| #514 | `cmd/cli/main.go` for `solo-provisioner` | "Create `cmd/cli/main.go` as the entry point for the `solo-provisioner` CLI binary, wiring up the existing Cobra command tree." | merged via #596 / #597 |
| #515 | `cmd/daemon/main.go` for `solo-provisioner-daemon` | "Create `cmd/daemon/main.go` as the entry point for the `solo-provisioner-daemon` binary with its own Cobra root command." | merged via #596 / #597 |
| #516 | Taskfile build targets for both binaries per platform | "Update `Taskfile.yaml` so build targets produce both `solo-provisioner` and `solo-provisioner-daemon` binaries for all target platforms (linux/amd64, linux/arm64)." | **this PR** |
| #517 | Independent release processes | "Update CI/CD pipeline to publish both `solo-provisioner` and `solo-provisioner-daemon` as named artefacts as separate release processes (each can be released independently and at different cadence)" | **this PR** |

The split-release framing is a deliberate departure from the original epic-doc design constraint #4 ("They may share the same release tag today, but the design preserves the option to ship them at different cadences ... later — no further pipeline restructure required"). Per recent direction from the issue update (#517 retitled to "independent release processes" on 2026-05-21), we're doing that restructure now rather than deferring it. The epic doc will be updated to reflect this.

## Problem

Two problems on `main` (post-#597):

**Problem 1 — Taskfile aggregators (#516).** Top-level `build` / `hash` / `sign` in `Taskfile.yaml:101-129` invoke only `:cli:all`, so `task build` produces no daemon binary, and `task hash` / `task sign` don't touch the daemon. The per-binary `hash:cli:all` / `sign:cli:all` / etc. families exist in `taskfiles/cli.yaml` and `taskfiles/daemon.yaml` (from #596) but have no `deps:` — calling `task hash:cli:all` directly without a prior `task build:cli:all` fails because `bin/solo-provisioner-*` doesn't exist yet. The dep chain only worked because the top-level `hash:` task had `deps: [build]`.

**Problem 2 — Release pipeline is single-binary (#517).** `.releaserc` ships one config that calls `task hash` (CLI-only) and `task sign` (CLI-only) and attaches `bin/*` to a single release tag (`v${version}`). The daemon binary has no release pipeline at all. `flow-deploy-release-artifact.yaml` knows nothing about the daemon.

## Decisions

| Question | Decision |
|---|---|
| Keep the top-level `task build`/`hash`/`sign` aggregators after splitting releases? | Keep `build:` only. It's the only one with a remaining caller (`zxc-code-compiles.yaml`) and serves as dev convenience for "compile both binaries in one command". `hash:` and `sign:` have **no caller** — release configs invoke per-binary `task hash:cli:all` / `task hash:daemon:all` (and the sign equivalents) directly, and dev workflows essentially never run them. Deleted to remove dead surface. `build:image:` had `deps: [hash]`; switched to `deps: [build]` (Dockerfile is not present in the repo today, so this task is dormant — kept consistent with what `build:` actually produces). |
| Add `deps:` to per-binary aggregators (`hash:cli:all` → `build:cli:all`)? | Yes. Necessary so the release configs can call `task hash:cli:all` standalone without prepending an explicit build. Symmetric on both binaries: `hash:cli:all` / `sign:cli:all` `deps: [build:cli:all]`; same for daemon. (The top-level `hash` keeps its existing `deps: [build]` — that's the same `build` task we updated for #516, which already calls both families.) |
| Shape of the `pkg/version` refactor? | **Symmetric subpackages.** `pkg/version/cli/` and `pkg/version/daemon/` each own a `VERSION`, `COMMIT`, embed file, generate script, and a thin `Get()` / `Cmd()` / `Print()` facade. Shared code (the `Info` struct, `Format`/`Text` methods, the `NewCmd(getter)` factory) lives at `pkg/version/`. CLI's existing files move from `pkg/version/{VERSION,COMMIT,releases.go,version_cmd.go,generate_*}` → `pkg/version/cli/…`. Daemon gets a parallel tree. |
| Where do the new semantic-release configs live? | `.releaserc_cli.json` and `.releaserc_daemon.json` at the repo root, mirroring the original `.releaserc` filename convention. Each workflow invokes `npx semantic-release --extends $(pwd)/.releaserc_{cli,daemon}.json`. The top-level `.releaserc` is **deleted** to avoid accidental fallback (cosmiconfig only auto-loads exactly `.releaserc[.json|.yaml|.yml|.js|.cjs]` — the suffixed variants are NOT auto-discovered, so explicit `--extends` is required and there's no ambiguity). |
| Tag namespaces? | `solo-provisioner-v${version}` (CLI) and `solo-provisioner-daemon-v${version}` (daemon). Both pipelines should stay in `0.x.y` — the `releaseRules: [{ breaking: true, release: "minor" }]` block (already in both `.releaserc_*.json` files) overrides the default `BREAKING CHANGE → major` so breaking commits stay in `0.x.y`. Bootstrapped by pre-pushing two seeding tags on origin: `solo-provisioner-v0.16.0` (continues CLI's existing lineage from the latest stable) and `solo-provisioner-daemon-v0.0.0` (so the daemon's first release lands at `0.0.1` / `0.1.0`, not `1.0.0`). Both seed at `v0.16.0`'s commit so the two pipelines see the same baseline. |
| Bootstrap the lineage in-PR or out-of-PR? | Out-of-PR. Pre-pushing tags from a PR is awkward (they wouldn't exist until merge); we document the bootstrap step in the plan's "Rollout" section so whoever merges the PR knows to push both seeding tags immediately after. |
| Workflow file shape? | Two independent `flow-deploy-release-*.yaml` entry workflows (CLI + daemon), each `workflow_dispatch`-only. Most of the body is duplicated between them (Go setup, GPG, npm install) — duplication chosen over a reusable `zxc-release.yaml` because the two pipelines' differences live in CLI args, paths, and labels, and a parameterized reusable workflow would mostly be `if: target == 'cli'` branches that obscure rather than DRY. The repo already has the `flow-*` (entry) vs `zxc-*` (reusable) convention; we're picking "two entries, no shared reusable" here because the parameterization gain is small. Revisit if a third release surface ever appears. |
| Delete or keep `flow-deploy-release-artifact.yaml`? | **Delete.** It assumes a single combined pipeline that no longer exists. Replaced by the two new entries. |
| `verifyRelease` exec for the version file write? | Each config writes to its own VERSION: CLI's `cmd: 'printf "%s" "${nextRelease.version}" > pkg/version/cli/VERSION'`; daemon's `pkg/version/daemon/VERSION`. |
| Should `verifyRelease` run `task build:cli:all && task hash:cli:all` or just `task hash:cli:all`? | Just `task hash:cli:all`. With the `deps: [build:cli:all]` we're adding to `hash:cli:all`, calling hash triggers build automatically. Same for daemon. |
| `prepare` exec for signing? | CLI: `task sign:cli:all`. Daemon: `task sign:daemon:all`. |
| Assets list per config? | 8 named entries each (binary + sha256 + sha256.asc + asc × amd64/arm64). The two configs together still produce 16 assets total — same coverage as the bundled approach, just split across two release tags. |
| `releaseRules`, `branches`, and `plugins` blocks shared between configs? | Duplicated verbatim in each config. JSON doesn't support imports/refs, so the only way to DRY would be to switch to `.cjs` or `.js` and `require()` a shared snippet — the configs are short enough that the duplication is more readable than that indirection. If they grow, revisit. |
| `pkg/version` `Cmd()` shape after refactor? | `pkg/version.NewCmd(getter func() Info) *cobra.Command` — a factory. Each subpackage's `Cmd()` calls `version.NewCmd(getter)` with its own `Get`. Caller-side: `versioncli.Cmd()` / `versiondaemon.Cmd()`. |
| Backwards compatibility for any code that imports `pkg/version` (i.e., not under `cmd/`)? | Grep confirms `pkg/version` is imported only from `cmd/cli/...` and `cmd/daemon/main.go`. The four import sites are: `cmd/cli/commands/root.go`, `cmd/cli/commands/common/run.go`, `cmd/cli/main.go` (if present), and `cmd/daemon/main.go`. All four get rewritten to point at the appropriate subpackage. No public API to preserve. |
| CI checks beyond release? | `zxc-code-compiles.yaml` still calls `task build` (top-level aggregator) — already produces both binaries via #516's wiring. `zxc-uat-test.yaml:463` still calls `task build:cli` explicitly — correct, UAT only exercises CLI. No CI workflow file changes for #517. |

## Scope

### Part A — Taskfile wiring (#516)

- [ ] `Taskfile.yaml` — top-level `build:` adds `- task: build:daemon:all`; `generates:` adds daemon globs. The explicit `- task: generate` is removed from `build:` cmds (now covered by sub-aggregator deps).
- [ ] `Taskfile.yaml` — delete top-level `hash:` and `sign:` aggregators (no remaining callers; release configs invoke per-binary tasks directly).
- [ ] `Taskfile.yaml` — `build:image:` switches `deps: [hash]` → `deps: [build]` (no Dockerfile present today; kept consistent with what survives).
- [ ] `Taskfile.yaml` — `generate:` task declared `run: once` so multiple callers in the same invocation share a single execution.
- [ ] `taskfiles/cli.yaml` — `build:cli:all` gets `deps: [generate]` so `pkg/version/cli/{VERSION,COMMIT}` exist before `go build ./cmd/cli`. `hash:cli:all` and `sign:cli:all` get `deps: [build:cli:all]`. Together this means standalone callers (release pipeline's `task hash:cli:all` / `sign:cli:all`) work on a clean checkout.
- [ ] `taskfiles/daemon.yaml` — symmetric: `build:daemon:all` gets `deps: [generate]`; `hash:daemon:all` and `sign:daemon:all` get `deps: [build:daemon:all]`.

### Part B — `pkg/version` refactor

- [ ] Create `pkg/version/cli/` and move:
  - `pkg/version/VERSION` → `pkg/version/cli/VERSION`
  - `pkg/version/COMMIT` → `pkg/version/cli/COMMIT`
  - `pkg/version/releases.go` → `pkg/version/cli/releases.go` (update package name to `cli`; embed paths stay relative)
  - `pkg/version/generate_unix.go` → `pkg/version/cli/generate_unix.go` (update package name)
  - `pkg/version/generate_windows.go` → `pkg/version/cli/generate_windows.go`
  - `pkg/version/generate_version_unix.sh` → `pkg/version/cli/generate_version_unix.sh`
  - `pkg/version/generate_version_windows.ps1` → `pkg/version/cli/generate_version_windows.ps1`
- [ ] Create `pkg/version/daemon/` with the same shape (symmetric mirror): VERSION (seeded `0.0.0`), COMMIT (placeholder, regenerated by `go generate`), `releases.go` with embeds, `generate_unix.go` + script, `generate_windows.go` + script.
- [ ] Refactor `pkg/version/info.go`:
  - Keep `Info` struct, `Format`, `Text`.
  - Remove the package-level `versionInfo` global and `init()` — Info construction moves into each subpackage's `Get()`.
- [ ] Refactor `pkg/version/version_cmd.go`:
  - `Cmd()` → `NewCmd(getter func() Info) *cobra.Command`.
  - `Print(cmd, format)` → `Print(cmd, format string, info Info) error`.
- [ ] Add `pkg/version/cli/cli.go` (or similar) exporting `Get() Info`, `Cmd() *cobra.Command`, `Print(cmd, format) error` — the three convenience wrappers callers expect.
- [ ] Add `pkg/version/daemon/daemon.go` with the symmetric helpers.
- [ ] Rewrite the four call sites:
  - `cmd/cli/main.go` — replace any `pkg/version` use with `pkg/version/cli`.
  - `cmd/cli/commands/root.go` — same.
  - `cmd/cli/commands/common/run.go` — same.
  - `cmd/daemon/main.go` — replace `pkg/version` with `pkg/version/daemon`.
- [ ] Sanity grep: `grep -rn "hashgraph/solo-weaver/pkg/version\"" --include='*.go' .` returns zero results outside `pkg/version/{cli,daemon}/`.

### Part C — semantic-release configs (#517)

- [ ] Create `.releaserc_cli.json`:
  - `tagFormat: "solo-provisioner-v${version}"`
  - `plugins:` same as old `.releaserc` minus `@semantic-release/github`'s assets entry (replaced)
  - `verifyRelease`: write `pkg/version/cli/VERSION`, then `task hash:cli:all`
  - `prepare`: `task sign:cli:all`
  - `@semantic-release/github` `assets:` = 8 named entries for CLI binaries
  - `releaseRules` and `branches` copied verbatim from old `.releaserc`
- [ ] Create `.releaserc_daemon.json` — symmetric, daemon-prefixed everywhere, writes to `pkg/version/daemon/VERSION`.
- [ ] Delete `.releaserc`.

### Part D — release workflows (#517)

- [ ] Create `.github/workflows/flow-deploy-release-cli.yaml`:
  - Header, harden-runner, checkout, Go setup, GPG setup, semantic-release npm install — same as old workflow.
  - "Publish Semantic Release" step runs `npx semantic-release --extends $(pwd)/.releaserc_cli.json`.
  - "Retrieve Release Version" step reads `pkg/version/cli/VERSION`.
  - Job name reflects "CLI" (e.g., `name: Github / Release / CLI`).
- [ ] Create `.github/workflows/flow-deploy-release-daemon.yaml` — symmetric, daemon paths, `--extends $(pwd)/.releaserc_daemon.json`, reads `pkg/version/daemon/VERSION`.
- [ ] Delete `.github/workflows/flow-deploy-release-artifact.yaml`.

### Part E — docs

- [ ] `CLAUDE.md` — update the "Key Conventions" output-path line to list both binaries, and add a one-line note that each binary has its own release pipeline / version file.
- [ ] `docs/claude/epics/00498-two-binary-build-layout.md`:
  - Status: `in progress` → `in review` (both remaining stories are in this PR).
  - Stories table: #514/#515 → `merged` (link to #596 + #597); #516/#517 → `in review` (link to this PR).
  - Design constraint #4: update to reflect that independent release is now done, not deferred.
  - References section: remove the `00516-…` / `00517-…` "pending" entries; point to this combined plan.

## Out of scope

- Code changes to either binary's behavior (no logic touched in `cmd/cli/...` or `cmd/daemon/...` except the version import path swap).
- Removing the stale `bin/solo-provisioner-darwin-*` globs in `Taskfile.yaml` — unrelated cleanup.
- Restructuring `pkg/version/info.go`'s `Info` struct or output formats — no fields added/changed.
- Pre-pushing the seeding tag `solo-provisioner-v0.16.0` — that's the rollout step (see below), not a PR file change.
- Adding scope-based release rules (e.g., "only release CLI on commits with scope `cli`") — semantic-release default behavior is fine for V1; revisit if cross-binary commit traffic causes spurious version bumps.
- A `zxc-release.yaml` reusable inner workflow — two entry workflows with some duplication are clearer than one parameterized reusable, given how small the inter-pipeline differences are.

## Rollout (post-merge, outside the PR)

1. Merge the rollup epic PR (this PR or a follow-up that brings the epic branch into main).
2. Push the seeding tags to origin so each pipeline starts in the desired range:
   ```bash
   git tag solo-provisioner-v0.16.0 v0.16.0
   git tag solo-provisioner-daemon-v0.0.0 v0.16.0
   git push origin solo-provisioner-v0.16.0 solo-provisioner-daemon-v0.0.0
   ```
   CLI continues from the existing `v0.16.0` lineage; daemon starts fresh at `0.0.0` so its first release lands at `0.0.1` / `0.1.0` rather than semantic-release's default `1.0.0`.
3. Sanity-check by manually triggering each workflow in `--dry-run-enabled=true` mode and confirming the predicted next versions (both should be `0.x.y`).

## Test plan

- [ ] Lint: `task lint:check` passes.
- [ ] License: `task license:check` passes for files added/modified by this PR (pre-existing missing-header failures in unrelated files are out of scope).
- [ ] Aggregated build (#516): `rm -rf bin && task build` produces all four binaries.
- [ ] Per-binary build standalone: `rm -rf bin && task hash:cli:all` triggers `build:cli:all` via the new dep, produces CLI binaries + `.sha256`. Same for `task hash:daemon:all`.
- [ ] Per-binary sign standalone (CI / GPG-enabled): `task sign:cli:all` produces `.asc` + `.sha256.asc` for CLI; `task sign:daemon:all` for daemon.
- [ ] `pkg/version` refactor — `go build ./...` succeeds; `go test -race -tags='!integration' ./...` passes.
- [ ] `solo-provisioner --version` and `solo-provisioner-daemon --version` both print their respective Info — CLI reads `pkg/version/cli/VERSION`, daemon reads `pkg/version/daemon/VERSION`. Smoke check: temporarily set one VERSION to a distinct sentinel (e.g., `9.9.9`), rebuild, confirm only that binary reports it.
- [ ] `.releaserc_cli.json` and `.releaserc_daemon.json` parse as valid JSON — `python3 -c "import json; json.load(open('.releaserc_cli.json'))"` and same for daemon.
- [ ] All 8 asset `path:` entries in `.releaserc_cli.json` map to files produced by `task build:cli:all && task hash:cli:all && task sign:cli:all`. Same for daemon's config + per-daemon tasks.
- [ ] Workflows lint: `actionlint .github/workflows/flow-deploy-release-cli.yaml` and same for daemon (if `actionlint` is available locally; otherwise smoke-check via YAML parser).
- [ ] Both workflows can be triggered in `--dry-run-enabled=true` mode without writing/pushing anything (this requires the workflow to actually exist on main — verifiable post-merge during rollout).

## Acceptance criteria

- [ ] `task build` produces both CLI and daemon binaries.
- [ ] `task hash:cli:all` (or `:daemon:all`) standalone produces hashes (build dep fires).
- [ ] `task sign:cli:all` / `:daemon:all` standalone produces signatures.
- [ ] `pkg/version/cli/` and `pkg/version/daemon/` are symmetric subpackages; each binary embeds and prints its own version.
- [ ] `.releaserc_cli.json` and `.releaserc_daemon.json` parse as valid JSON; each lists 8 named assets with `path:` and `label:`.
- [ ] `.github/workflows/flow-deploy-release-cli.yaml` and `flow-deploy-release-daemon.yaml` exist; `flow-deploy-release-artifact.yaml` is deleted.
- [ ] `.releaserc` is deleted.
- [ ] `grep -rn "hashgraph/solo-weaver/pkg/version\"" --include='*.go' .` returns no results outside `pkg/version/{cli,daemon}/`.
- [ ] `CLAUDE.md` and `docs/claude/epics/00498-two-binary-build-layout.md` updated.

## Risks / rollbacks

- **Risk — pre-rollout tag seeding forgotten.** If either `solo-provisioner-v0.16.0` or `solo-provisioner-daemon-v0.0.0` isn't pushed before the corresponding first workflow run, semantic-release will default to `1.0.0` for that pipeline — a version jump we don't want (CLI would lose its `0.16.x` lineage; daemon would skip past `0.x.y` entirely). Mitigated: rollout step is documented above and called out in the PR description. The first workflow run can also be done in `--dry-run-enabled=true` mode to inspect the predicted next version before committing.
- **Risk — `pkg/version` import sweep miss.** A missed `pkg/version` (without subpackage) import in code that should target `cli` would either fail to compile or compile against the empty parent package. Mitigated: sanity grep in the test plan.
- **Risk — release-config typo (`path:` mismatch).** semantic-release fails at upload time, not at PR-check time. Mitigated: per-config asset existence check in the test plan.
- **Risk — cross-binary commit traffic inflates version bumps.** With no scope filtering, a release workflow run will compute the next version off all commits since the last tag in its namespace — even commits that only touched the other binary. Mitigated as accepted limitation; revisit by adding `releaseRules` scope filters if it becomes a problem.
- **Risk — concurrent workflow runs on the same commit.** Both pipelines could fire simultaneously and race on writing different VERSION files. Mitigated: they write to different files; semantic-release tags via `git push` are atomic; no shared state to clobber.
- **Rollback:** revert the merge commit. Both new workflows disappear; the deleted ones re-materialize. The seeding tag `solo-provisioner-v0.16.0` on origin is harmless if left behind (no workflow references it post-revert), and can be deleted manually.

## References

- Issues: #516 (Taskfile aggregator wiring), #517 (independent release processes — retitled 2026-05-21)
- Epic: #498 — Two-Binary Build Layout (`docs/claude/epics/00498-two-binary-build-layout.md`)
- Sibling story plan: `docs/claude/plans/00514-cli-main-entry-point.md`
- Sibling story bodies: #514, #515 (merged in #596 via rollup #597)
- Current code (pre-PR):
  - `Taskfile.yaml:101-129` — top-level `build`/`hash`/`sign` aggregators
  - `taskfiles/cli.yaml` — full `build:cli:*` / `hash:cli:*` / `sign:cli:*` families
  - `taskfiles/daemon.yaml` — full `build:daemon:*` / `hash:daemon:*` / `sign:daemon:*` families
  - `pkg/version/` — single-binary version package (will be refactored)
  - `.releaserc` — single-release config (will be deleted)
  - `.github/workflows/flow-deploy-release-artifact.yaml` — single-release workflow (will be deleted)
  - Latest stable release tag on origin: `v0.16.0`
