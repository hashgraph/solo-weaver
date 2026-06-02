# #628 — Review guide: enforce errorx as the standard for error construction

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/628
> **Branch:** `00628-enforce-errorx`
> **Base:** `main`

## Problem and solution

`github.com/joomcode/errorx` was adopted as the standard error library by PR #15 (2025-07-24), but without a CI gate the codebase drifted — 105 `fmt.Errorf` + 1 `errors.New` re-accumulated across 24 production files. This PR delivers all three parts of the chewie rollout (mirroring swirldslabs/chewie#232 + #233) in one branch:

1. **Lint gate** — `.golangci.yml` v2 with `forbidigo` forbidding `errors.New` and `fmt.Errorf` in non-test, non-generated Go files, scoped to lines new vs `origin/main`. Wired into `task lint` and `task lint:check` via a new `lint:errorx` task, with `setup:golangci-lint` pinning `v2.12.2`.
2. **Mass migration** — 106 call sites converted to the appropriate `errorx.<Type>` namespace per the mapping table in the plan.
3. **Claude skill** — `.claude/skills/errorx-conventions/SKILL.md` captures the rule, namespace mapping, `errors.Join` sentinel pattern, and the migration traps.

CI checkout now uses `fetch-depth: 0` so `new-from-rev: origin/main` can resolve.

## Changed files

| File | What changed |
|---|---|
| `.golangci.yml` (new) | v2 config; only `forbidigo` enabled; SPDX header; "solo-weaver standard" guidance messages; `new-from-rev: origin/main` |
| `Taskfile.yaml` | Added `setup:golangci-lint` (pinned v2.12.2, version-aware `status` check), `lint:errorx` (deps: `setup:golangci-lint`, `generate`, `mocks`; env `GOOS=linux`); wired into `lint` + `lint:check` |
| `.github/workflows/zxc-code-compiles.yaml` | `actions/checkout` now uses `fetch-depth: 0` |
| `.claude/skills/errorx-conventions/SKILL.md` (new) | New skill; same shape as `sanity-validators` / `taskfile-conventions` |
| `docs/claude/plans/00628-enforce-errorx.md` (new) | Plan file capturing decisions and scope boundaries |
| `cmd/cli/commands/alloy/cluster/install.go` | 4× `fmt.Errorf` → `errorx.IllegalArgument` (CLI flag validation) |
| `cmd/cli/commands/alloy/cluster/parse_remote.go` | 4× `fmt.Errorf` → `errorx.IllegalArgument` (key=value parsing) |
| `cmd/cli/commands/block/node/init.go` | 1× `fmt.Errorf` → `errorx.IllegalArgument` (`--plugins` required when preset=Custom) |
| `cmd/cli/commands/common/flags.go` | 5× converted; unsupported-type defaults → `errorx.AssertionFailed`, flag-not-found → `errorx.IllegalArgument` |
| `cmd/cli/commands/common/run.go` | 2× converted; TUI model type-assertion → `errorx.AssertionFailed`, step-failed → `errorx.IllegalState` |
| `cmd/cli/commands/demo.go` | 1× converted; simulated failure → `errorx.IllegalState` |
| `internal/alloy/config.go` | 1× `errorx.ExternalError.Wrap` for `os.Hostname` failure |
| `internal/alloy/render.go` | 7× `errorx.InternalError.Wrap` for template render failures |
| `internal/network/network.go` | 1× `errorx.IllegalState.New("no connected network interface found")` |
| `internal/state/cluster_state.go` | 2× converted; helm manager init → `errorx.InternalError`, list releases → `errorx.ExternalError` |
| `internal/ui/prompt/blocknode.go` | 7× `errorx.IllegalArgument.New` (form validation closures) |
| `internal/ui/prompt/prompt.go` | `ErrAborted` sentinel rewired to `errorx.RejectedOperation.New(...)`; `wrapFormError` returns `errorx.ExternalError.Wrap` |
| `internal/workflows/steps/catalog.go` | 4× converted across catalog-resolution errors (`IllegalFormat` / `IllegalArgument`) |
| `internal/workflows/steps/step_alloy.go` | 9× converted; secret read/endpoint reach failures → `errorx.ExternalError`, secret-missing → `errorx.IllegalState` |
| `internal/workflows/steps/step_cluster_crds.go` | 1× CRD-meta failure → `errorx.InternalError` |
| `internal/workflows/steps/step_kubeadm.go` | 4× converted; kubeadm image-pull/init/configure → `errorx.ExternalError`, kubeconfig manager init → `errorx.InternalError` |
| `pkg/hardware/base_node.go` | 5× hardware-requirement failures → `errorx.IllegalState` |
| `pkg/hardware/node_spec.go` | 1× unknown node type/profile combo → `errorx.IllegalArgument` |
| `pkg/helm/manager.go` | 3× chart-dependency and reload-after-update failures → `errorx.ExternalError`; `fmt` import removed |
| `pkg/security/principal/provider_utils_unix.go` | 8× `/etc/passwd` and `/etc/group` parsing → `errorx.IllegalFormat` |
| `pkg/security/sudoers/sudoers.go` | 5× converted; syntax validation → `errorx.IllegalFormat`, write failure → `errorx.ExternalError`; `fmt` import removed |
| `pkg/software/config.go` | 12× catalog YAML validation failures → `errorx.IllegalFormat` / `errorx.IllegalArgument` |
| `pkg/software/downloader.go` | 7× converted; redirect-rejection → `errorx.RejectedOperation`, tar-extraction integrity violations → `errorx.IllegalState` (wrapped by existing `NewExtractionError` constructor) |
| `pkg/software/kubeadm_installer.go` | 1× crypto/rand failure → `errorx.ExternalError` |

## Code review checklist

- [ ] **Lint gate is scoped correctly.** Negative test: temporarily add `_ = errors.New("test")` to any non-test production file, confirm `task lint:check` fails with the configured message; revert.
- [ ] **`new-from-rev: origin/main` works in CI.** The workflow's `fetch-depth: 0` change is what makes this possible; sanity-check the diff against `main` in the GitHub Actions log on the first CI run.
- [ ] **Namespace mapping is consistent with the table.** Sample-check 5 files across different shape categories:
  - `pkg/helm/manager.go` — external (helm calls) → `ExternalError`
  - `pkg/software/config.go` — schema/format validation → `IllegalFormat`
  - `internal/workflows/steps/step_alloy.go` — mixed external/state shapes
  - `cmd/cli/commands/common/flags.go` — programmer-error type-switch defaults → `AssertionFailed`
  - `internal/ui/prompt/prompt.go` — user cancel sentinel → `RejectedOperation`
- [ ] **`ErrAborted` semantics preserved.** `var ErrAborted = errorx.RejectedOperation.New("aborted by user")` is identity-comparable; `errors.Is(err, ErrAborted)` still matches whether `err` is `ErrAborted` directly or has it joined into a multi-error. There are no current external callers doing `errors.Is(..., prompt.ErrAborted)`, but the contract is intentionally preserved for future use.
- [ ] **`fmt` imports**: every file that converted away from `fmt.Errorf` either drops the `fmt` import entirely (verify: `goimports` did not flag anything) or keeps it because the file still uses `fmt.Sprintf` / `fmt.Fprint*` / `fmt.Printf`. Spot-check `pkg/helm/manager.go` (removed), `pkg/security/sudoers/sudoers.go` (removed), `pkg/hardware/base_node.go` (kept for `fmt.Sprintf`), `internal/ui/prompt/blocknode.go` (kept for `fmt.Sprintf` in the multi-select description).
- [ ] **Special wrapper preserved**: `NewExtractionError(errorx.IllegalState.New(...), ...)` in `pkg/software/downloader.go` — confirm the outer constructor is untouched and the inner stdlib `fmt.Errorf` is now an errorx value.
- [ ] **Skill triggers reliably.** Description includes the literal tokens `errors.New`, `fmt.Errorf`, `errorx` so a fresh Claude session picks it up before generating new error construction.
- [ ] **`task lint:errorx` developer ergonomics**: deps run `generate` and `mocks` so a fresh checkout / clean cache produces a clean lint without manual setup. `GOOS=linux` env makes the linter see the same code CI does (`internal/mount`, `pkg/kernel/module` typecheck OK).

## Test commands

```bash
# Lint pipeline (gate, format, vet)
task lint:check

# Full unit test suite (Linux-only paths require the VM)
task vm:test:unit

# Targeted unit tests for changed packages
go test -race -tags='!integration' ./pkg/hardware/... ./pkg/helm/... ./pkg/security/... \
    ./internal/alloy/... ./internal/network/... ./internal/state/... ./internal/ui/prompt/... \
    ./internal/workflows/steps/...

# Integration smoke tests (UTM VM)
task vm:test:integration TEST_NAME='^Test_StepKubeadm_Fresh_Integration$'
task vm:test:integration TEST_NAME='^Test_StepAlloy_'
task vm:test:integration TEST_NAME='^Test_BlockNode_'

# Negative test for the lint gate (manual)
# 1. Add `_ = errors.New("test")` to any production .go file
# 2. Run: task lint:check  → expect non-zero exit with forbidigo message
# 3. Revert the change
```

## Manual UAT

```bash
# 1. Confirm setup task installs golangci-lint v2.12.2
golangci-lint --version
# Expect: golangci-lint has version v2.12.2 built with ...

# 2. Confirm task aggregator runs the gate
task lint:check
# Expect tail output:
#   task: [lint:errorx] golangci-lint run --config .golangci.yml ./...
#   [lint:errorx] 0 issues.

# 3. Confirm task lint (auto-format path) also runs the gate
task lint
# Expect the same "0 issues" tail.

# 4. End-to-end sanity (in UTM VM)
solo-provisioner kube cluster install --profile local
solo-provisioner block node install
# Watch for any error path; confirm the doctor layer's styled output still
# renders the errorx namespace icon / category correctly.
```

## Risks and rollback

- **`new-from-rev` boundary edge case.** Contributors who branch from a non-main starting point may see false positives the first time CI runs. The `lint:errorx` task `git fetch origin main`s before invoking the linter to keep this rare; if it bites, revert just the workflow + Taskfile changes — the `.golangci.yml` alone is inert without the task wiring.
- **`ErrAborted` regression.** Identity-comparison still works because the variable is declared once at package scope. If a downstream callee starts re-creating the value per call, `errors.Is` would silently stop matching. The skill calls this out under "Migration traps #3".
- **Per-file rollback.** Each file's migration is mechanical and isolated; a wrong namespace can be reverted on its own without affecting the lint gate or other files.
