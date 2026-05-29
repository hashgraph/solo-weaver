# #628 — Enforce errorx as the standard for error construction

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/628
> **Story branch:** `00628-enforce-errorx`
> **PR base:** `main` (branched from `origin/main` @ `3866a9c`)
> **PR closes:** #628 — all three deliverables (lint gate + migration + skill) land in this single PR

## Summary

`github.com/joomcode/errorx` was adopted as the standard error library by PR #15 (2025-07-24), but with no CI gate the codebase has drifted — 105 `fmt.Errorf` and 1 `errors.New` call sites have re-accumulated in production Go code under `cmd/`, `internal/`, `pkg/`. This work re-enforces the standard by mirroring the chewie rollout (swirldslabs/chewie#232, #233) in three deliverables: a `forbidigo` lint gate scoped to new code, a mass migration of the 106 pre-existing call sites, and a Claude skill that documents the namespace-mapping decision matrix so future code starts conformant.

## Problem

Today's situation (recounted in this branch as of `origin/main`):

| Construct | Count | Where |
|---|---|---|
| `errorx.<Type>.<New\|Wrap>` | 100+ files already import `errorx` | broad coverage |
| `fmt.Errorf(` | **105 call sites across 24 files** | `cmd/cli/commands/**`, `internal/{alloy,network,state,ui,workflows}/**`, `pkg/{hardware,helm,security,software}/**` |
| `errors.New(` | **1 call site** | `internal/ui/prompt/prompt.go:30` (`var ErrAborted = errors.New("aborted by user")`) |

The issue body cites slightly different numbers (118 errorx / 114 stdlib) — close enough; the gap is normal drift between issue authoring and today. Confirmed exact counts to use for the migration PR's "before / after" claim.

Three concrete pain points:

1. **No lint gate.** `task lint` is `go fmt -x ./...` and `task lint:check` adds nothing else — there is no mechanism blocking a contributor (or Claude) from typing `fmt.Errorf` again.
2. **Drift after one prior migration.** PR #15 already did this migration once. Without enforcement it slowly reversed; another mass rewrite is wasted effort unless the gate lands first.
3. **No documented namespace mapping.** Existing `errorx` usage in the repo is uneven: `IllegalState` (359 instances), `IllegalArgument` (292), `InternalError` (97), `IllegalFormat` (28), `ExternalError` (14). Some shapes that should map to `ExternalError` (file I/O, shell exec, helm) currently use `IllegalState`. The skill captures the intended mapping so converters and reviewers can disagree against a written rule, not vibes.

## Decisions

| Question | Decision |
|---|---|
| Land as one PR or three? | **One PR** (overriding the issue's "ideally separate PRs" preference per user direction). The gate + migration are tightly coupled — landing the gate without the migration leaves contributors unable to add lines near pre-existing `fmt.Errorf` neighborhoods without picking them up under `new-from-rev`. Bundling avoids that awkward window. |
| Use chewie's `.golangci.yml` verbatim or adapt? | **Adapt** — copy structure, swap "chewie standard" → "solo-weaver standard" in the `forbidigo` messages. Same `forbidigo` patterns, same `analyze-types: true`, same exclusions, same `new-from-rev: origin/main`. Add SPDX header (this repo enforces them; chewie's already has one). |
| Pin `golangci-lint` version? | **Yes, v2.12.2** — same pin as chewie. Locks reproducibility; bumps are explicit. |
| Where do `setup:golangci-lint` and `lint:errorx` live — `Taskfile.yaml` root or a new `taskfiles/lint.yaml`? | **Root `Taskfile.yaml`.** The existing `lint` / `lint:check` are already at the root and tiny (3 lines each). Extracting now would just spread two tasks across two files. The taskfile-conventions skill rule of thumb: extract when a family grows past ~30 lines or develops its own OS/arch fan-out. We're not there yet. |
| Wire `lint:errorx` into `lint` (auto-format) or only `lint:check` (CI)? | **Both** — match chewie's structure. `lint` is the "make my changes pass" entry; `lint:check` is the CI verification. If a contributor runs `task lint` and a forbidigo finding appears, they want to know now, not when CI fails. |
| What to do about the lone `errors.New` sentinel `ErrAborted` in `prompt.go`? | Convert to `errorx.RejectedOperation.New("aborted by user")` in the migration PR. `errorx` types still satisfy `error` and `errors.Is(err, ErrAborted)` works as long as `ErrAborted` is the exact value being checked, which is how it's used today. Verify the one caller (`fmt.Errorf("prompt error: %w", err)` at `prompt.go:89`) — that becomes `errorx.ExternalError.Wrap(err, "prompt error")` (user-facing prompt = external I/O). |
| How to handle `NewExtractionError(fmt.Errorf(...), ...)` patterns in `pkg/software/downloader.go`? | The inner `fmt.Errorf` is being consumed by a custom constructor `NewExtractionError`. Convert each inner call to `errorx.IllegalState.New(...)` (these are integrity-violation conditions: "path traversal attempt", "absolute symlink not allowed", etc. — they signal corrupted/malicious archive state). `NewExtractionError` itself stays untouched. |
| Custom `errorx.NewNamespace` declarations already in the repo (e.g. `pkg/os/errors.go`, `pkg/helm/errors.go`, `pkg/fsx/errors.go`)? | **Leave them alone.** They predate this work and are correct. The skill discourages *new* custom namespaces, not existing ones. |
| Should the lint config also flag `errors.Is`, `errors.As`, `errors.Unwrap`? | **No** — those are stdlib utilities that work with `errorx` errors too (errorx implements the `Unwrap()` contract). The skill explicitly calls out that `errors.Is` / `errors.As` remain the right way to test sentinel/typed errors. |
| Where does the Claude skill live? | `.claude/skills/errorx-conventions/SKILL.md` — same shape as the existing `sanity-validators` and `taskfile-conventions` skills (single `SKILL.md` file, no helpers). |

## Scope

### Part 1 — Lint gate

- [ ] New file: `.golangci.yml` at repo root. Mirror swirldslabs/chewie#232 with these edits:
  - Add `# SPDX-License-Identifier: Apache-2.0` as the first comment line.
  - Replace "chewie standard" → "solo-weaver standard" in both `forbid` messages.
  - Keep `forbidigo` as the only enabled linter, `default: none`, same `analyze-types: true`, same `exclude-godoc-examples: true`.
  - Keep the exclusions: `_test\.go`, `(\.gen\.go|_generated\.go|/generated/)$`.
  - Keep `issues.new-from-rev: origin/main`.
- [ ] `Taskfile.yaml` root: add tasks
  - `setup:golangci-lint` (internal, `status: which golangci-lint`, installs `github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2`).
  - `lint:errorx` (internal, deps `setup:golangci-lint`, fetches `origin/main` then runs `golangci-lint run --config .golangci.yml ./...`).
  - Wire `lint:errorx` into both `lint` and `lint:check` (after the existing `go fmt` lines).
- [ ] `.github/workflows/zxc-code-compiles.yaml`: add `with: fetch-depth: 0` to the existing `actions/checkout` step (line 36) so `new-from-rev: origin/main` can resolve the base.
- [ ] **No production code changes in this PR.** Verify on this branch that `task lint:check` runs cleanly (since the rule only flags lines changed relative to `origin/main`, and this branch only touches lint infra, there should be zero findings).

### Part 2 — Mass conversion

- [ ] Convert the 105 `fmt.Errorf` call sites across the 24 production files (see file list below) using the mapping below. **Per-file commits** so each can be reviewed independently — at this scale, a single 100-site commit is unreviewable.
- [ ] Convert the 1 `errors.New` site in `internal/ui/prompt/prompt.go:30` (`ErrAborted`).
- [ ] Replace any `fmt.Errorf("%w: %w", sentinel, cause)` patterns with `errors.Join(sentinel, cause)` (none found in current scan, but a grep over the changed lines as a safety net is cheap).
- [ ] After each file's conversion, run `task lint` to keep `goimports`/`gofmt` happy. After the full migration, verify:
  - `grep -rE 'errors\.New\(|fmt\.Errorf\(' --include='*.go' cmd internal pkg | grep -v _test.go` → **0** matches.
  - `task lint:check` passes.
  - `task test:unit` passes (still on macOS for the OS-portable subset).
  - In the VM: `task vm:test:unit` and `task vm:test:integration TEST_NAME='…'` for any handlers touched.

**Namespace mapping (this is the canonical version — the skill copies from here):**

| Call site shape | `errorx` target |
|---|---|
| `os.Open`/`os.Stat`/`os.ReadFile` failure; shell/exec command failure; helm CLI failure; kubectl/kube API failure; GitHub API failure; DB query | `errorx.ExternalError.Wrap(err, "…")` |
| Template render failure; internal `text/template` / `html/template` execute; expected-impossible internal computation that did error | `errorx.InternalError.Wrap(err, "…")` |
| `json.Unmarshal` / `yaml.Unmarshal` / `toml.Unmarshal` failure; malformed checksum string; bad version string format | `errorx.IllegalFormat.Wrap(err, "…")` or `.New(…)` |
| Invariant breach ("should never happen", "redirect loop", "unreachable") | `errorx.AssertionFailed.New(…)` |
| User CLI flag value or config-file value rejected at validation | `errorx.IllegalArgument.New(…)` (or `.Wrap` if there's an upstream parse error to wrap) |
| Permission / authorization / user-cancelled-operation | `errorx.RejectedOperation.New(…)` |
| Inconsistent runtime state (unexpected enum, wrong phase, missing required field at a point it should have been set) | `errorx.IllegalState.New(…)` |

**Files in scope (24):**

```
cmd/cli/commands/alloy/cluster/install.go
cmd/cli/commands/alloy/cluster/parse_remote.go
cmd/cli/commands/block/node/init.go
cmd/cli/commands/common/flags.go
cmd/cli/commands/common/run.go
cmd/cli/commands/demo.go
internal/alloy/config.go
internal/alloy/render.go
internal/network/network.go
internal/state/cluster_state.go
internal/ui/prompt/blocknode.go
internal/ui/prompt/prompt.go
internal/workflows/steps/catalog.go
internal/workflows/steps/step_alloy.go
internal/workflows/steps/step_cluster_crds.go
internal/workflows/steps/step_kubeadm.go
pkg/hardware/base_node.go
pkg/hardware/node_spec.go
pkg/helm/manager.go
pkg/security/principal/provider_utils_unix.go
pkg/security/sudoers/sudoers.go
pkg/software/config.go
pkg/software/downloader.go
pkg/software/kubeadm_installer.go
```

**Special cases identified during exploration:**

- `pkg/software/downloader.go` — wraps `fmt.Errorf` inside `NewExtractionError(...)`. Convert the inner argument to `errorx.IllegalState.New(...)` (integrity-violation conditions). `NewExtractionError` itself is unchanged.
- `internal/ui/prompt/prompt.go:30` — `var ErrAborted = errors.New("aborted by user")` becomes `var ErrAborted = errorx.RejectedOperation.New("aborted by user")`. Existing `errors.Is(err, ErrAborted)` callers continue to work.
- `internal/ui/prompt/prompt.go:89` — `fmt.Errorf("prompt error: %w", err)` becomes `errorx.ExternalError.Wrap(err, "prompt error")` (user-input I/O failure, e.g. terminal closed).
- Several files retain `fmt.Sprintf` after the conversion. Keep the `fmt` import in those files; only remove `fmt` when no other `fmt.*` call remains.

### Part 3 — Claude skill `errorx-conventions`

- [ ] New file: `.claude/skills/errorx-conventions/SKILL.md`. Follow the structure of `.claude/skills/sanity-validators/SKILL.md`:
  - Frontmatter (`name`, `description`). The `description` triggers when Claude is about to write/modify Go error construction — phrased to match the triggering style of `sanity-validators` ("Use this skill BEFORE writing or modifying any … in this repo — e.g. \"…\", \"…\", any edit that …"). Include the literal tokens `errors.New`, `fmt.Errorf`, and `errorx` so the description-matcher picks it up reliably.
  - Sections:
    1. **The rule** — "no `errors.New` / `fmt.Errorf` in production code; `*_test.go` is exempt." Cite the `forbidigo` lint and where the config lives.
    2. **Namespace mapping table** — copy the canonical table from this plan.
    3. **Decision matrix** — short solo-weaver-specific examples: `os.Stat` failure → `ExternalError`; bad CLI flag → `IllegalArgument`; `kubectl` exec failure → `ExternalError`; helm install failure → `ExternalError`; unexpected enum value → `AssertionFailed`; user pressed Ctrl-C in prompt → `RejectedOperation`; template execute failure → `InternalError`.
    4. **Sentinel pattern** — `errors.Join(sentinel, cause)` instead of `fmt.Errorf("%w: %w", sentinel, cause)`. Sentinels themselves should be `errorx.<Type>.New(...)` typed values. Note that `errors.Is` walks the joined slice so checking either side works.
    5. **`errors.Is` / `errors.As` are still allowed** — they're stdlib utilities that work with `errorx`. The skill is about *constructing* errors, not testing them.
    6. **Tests are exempt and why** — rich error typing in tests obscures the assertion; tests should fail on the value/shape they expect.
    7. **No new custom namespaces** — stick to built-in `errorx.<Type>` namespaces. Existing custom namespaces in `pkg/os/errors.go`, `pkg/helm/errors.go`, `pkg/fsx/errors.go` are grandfathered; don't add more.
    8. **Common refactoring traps** — keep the `fmt` import if `fmt.Sprintf` is still used; don't fold `errorx.Decorate` (legacy) into `Wrap` (we use `Wrap` only).
- [ ] Add a one-line entry to the root `CLAUDE.md` "Key Conventions" section linking the skill (mirroring how the sanity skill is implicitly discoverable). Actually — review the existing CLAUDE.md: skills are auto-discovered from `.claude/skills/*/SKILL.md`, so no explicit link is needed. Skip this step unless the user disagrees.

## Out of scope

- Replacing existing custom-namespace declarations in `pkg/os/errors.go`, `pkg/helm/errors.go`, `pkg/fsx/errors.go` with built-in namespaces. They work and removing them is its own design question (loses type-safe `errorx.IsOfType` checks).
- Converting `*_test.go` files. The lint exempts them by design.
- Adding additional `golangci-lint` checkers (e.g. `staticcheck`, `govet` beyond the existing inline rules, `gosec`). The lint config is intentionally narrow — broadening it would require a pre-existing-issues cleanup that this issue doesn't fund.
- Wiring `lint:errorx` into the daemon-specific CI flow (`flow-deploy-release-daemon.yaml`) — release workflows don't run `task lint:check` today and adding it is a separate concern.
- Touching `internal/doctor/` exit-code mapping to take advantage of the now-uniform errorx namespaces. Worthwhile follow-up, but separate issue.

## Test plan

### Part 1 (lint gate)

- [ ] `task setup:golangci-lint` installs the binary.
- [ ] `task lint:errorx` runs cleanly against the current `main` (since `new-from-rev: origin/main` scopes findings to changed lines; only `.golangci.yml` / `Taskfile.yaml` / workflow change here).
- [ ] Negative test: temporarily add a `_ = errors.New("test")` to any non-test file, confirm `task lint:errorx` fails with the configured message. Revert before commit.
- [ ] `task lint:check` (full pipeline) passes.
- [ ] `task build` still passes — `golangci-lint` shouldn't affect compilation.
- [ ] CI: push the branch, confirm the new "Code Style" step still runs and that `fetch-depth: 0` doesn't break any other step.

### Part 2 (mass conversion)

- [ ] `grep -rE 'errors\.New\(|fmt\.Errorf\(' --include='*.go' cmd internal pkg | grep -v _test.go` → 0 matches.
- [ ] `task lint:check` clean.
- [ ] `task test:unit` clean on macOS (covers the OS-portable subset).
- [ ] `task vm:test:unit` clean (full unit suite incl. Linux-only packages).
- [ ] `task vm:test:integration` clean — at least the workflows that touch the changed files (`Test_StepAlloy_*`, `Test_StepKubeadm_*`, `Test_StepClusterCRDs_*`, `Test_BlockNode_*`).
- [ ] Spot-check sentinel behavior: `errors.Is(err, prompt.ErrAborted)` still works after `ErrAborted` becomes an `errorx` value (Go playground / a quick unit test).
- [ ] Manual UAT: run `solo-provisioner` against the VM end-to-end (`kube cluster install` → `block node install`) and confirm error paths produce sensible messages — typed errors flow through `internal/doctor/` which formats them via `errorx`'s stack-trace-aware printer.

### Part 3 (skill)

- [ ] Manually trigger the skill in a fresh Claude session: ask Claude to add a `fmt.Errorf("foo: %w", err)` somewhere; confirm it picks up the skill and writes `errorx.<Type>.Wrap(...)` instead.
- [ ] Confirm the frontmatter `description` triggers on the right keywords by inspecting the skill listing (`/skills` or harness output) in a session where the user mentions error handling.

## Risks / rollbacks

- **Lint gate — `new-from-rev: origin/main` boundary edge case.** If a contributor branches off a different branch (e.g. an epic branch), `golangci-lint` may not have `origin/main` as a direct ancestor and could report false positives. Mitigation: the `lint:errorx` task explicitly `git fetch origin main`s before invoking the linter. Rollback: revert the workflow + Taskfile change; the config file alone is inert.
- **Migration — semantic drift during 100+ conversions.** A wrong namespace doesn't break behavior but loses signal for `internal/doctor/` and any future `errorx.IsOfType` callers. Mitigation: per-file edits + reviewer can sample-check the trickiest 5-10 files. The migration is mechanical, so file-level reverts are safe if a specific conversion was wrong.
- **Migration — `ErrAborted` becomes an `errorx` value.** Existing `errors.Is(err, ErrAborted)` callers compare against the variable's identity, which Go's `errors.Is` does via `==` on terminal errors. `errorx` errors implement `Is(target error) bool` to handle wrapping, so identity check still works as long as the variable itself is preserved. Mitigation: add a tiny unit test pinning the `errors.Is(ErrAborted, ErrAborted) == true` invariant before the change.
- **Skill — description doesn't trigger.** If the description string doesn't include the tokens Claude's harness greps for, the skill won't load when needed. Mitigation: literally include `errors.New`, `fmt.Errorf`, and `errorx` in the description; mirror the sanity-validators description structure (which is known to trigger reliably).
