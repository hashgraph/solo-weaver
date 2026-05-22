# Review guide — #516 + #517 (epic #498)

> **Branch:** `00516-taskfile-daemon-build-targets`
> **PR base:** `00498-feat-two-binary-build-layout`
> **Closes:** #516, #517
> **Plan:** [`docs/claude/plans/00516-taskfile-daemon-build-targets.md`](../plans/00516-taskfile-daemon-build-targets.md)

## Problem and solution

After #514/#515 (PR #596, rolled up via #597), `cmd/cli/main.go` and `cmd/daemon/main.go` exist along with per-binary task families in `taskfiles/cli.yaml` and `taskfiles/daemon.yaml`. Two gaps remained:

1. The top-level `task build` / `hash` / `sign` aggregators only invoked the `:cli:all` variants — `task build` produced no daemon binary.
2. The release pipeline (`.releaserc` + `flow-deploy-release-artifact.yaml`) was single-binary and bound to a single tag namespace (`vX.Y.Z`). The daemon had no release pipeline at all.

This PR:

- **#516 — aggregator wiring.** Top-level `build:` now invokes both `build:cli:all` and `build:daemon:all`; `sources:`/`generates:` globs gain daemon entries. The top-level `hash:` and `sign:` aggregators are **deleted** — they had no remaining callers, since each release pipeline invokes the per-binary tasks (`task hash:cli:all`, `task sign:cli:all`, daemon equivalents) directly. Per-binary aggregators gain `deps: [build:*:all]` so they're self-sufficient, and `build:cli:all` / `build:daemon:all` gain `deps: [generate]` so the embedded `pkg/version/{cli,daemon}/{VERSION,COMMIT}` files exist on a clean checkout. The `generate:` task is declared `run: once` so multiple deps in one invocation share a single execution.
- **#517 — independent release processes.** Replace the single config + workflow with two independent semantic-release pipelines: `.releaserc_cli.json` + `flow-deploy-release-cli.yaml` (tag namespace `solo-provisioner-vX.Y.Z`), and `.releaserc_daemon.json` + `flow-deploy-release-daemon.yaml` (tag namespace `solo-provisioner-daemon-vX.Y.Z`). Each workflow is `workflow_dispatch`-only and writes to its binary's VERSION file (`pkg/version/cli/VERSION` or `pkg/version/daemon/VERSION`).
- **`pkg/version` refactor.** Split into symmetric subpackages so each binary embeds its own VERSION/COMMIT. Shared code under `internal/...` keeps importing the parent `pkg/version`; each subpackage registers its Info with the parent at init() so `version.Get`/`Number`/`Commit` returns the running binary's value.

## Changed files

| File | What changed |
|---|---|
| `Taskfile.yaml` | Top-level `build:` invokes both `build:cli:all` and `build:daemon:all`; daemon globs added to `sources:`/`generates:`. Top-level `hash:` and `sign:` aggregators deleted (no remaining callers). `generate:` declared `run: once`. `build:image:` deps retargeted `hash` → `build`. |
| `taskfiles/cli.yaml` | `build:cli:all` gains `deps: [generate]`; `hash:cli:all` and `sign:cli:all` gain `deps: [build:cli:all]` so each is self-sufficient on a clean checkout. |
| `taskfiles/daemon.yaml` | Symmetric: `build:daemon:all` deps `generate`; `hash:daemon:all` and `sign:daemon:all` deps `build:daemon:all`. |
| `pkg/version/info.go` | Adds runtime-current Info accessors (`SetCurrent`, `Get`, `Number`, `Commit`) so shared code reads the running binary's version. |
| `pkg/version/version_cmd.go` | `Cmd()` becomes `NewCmd(getter)` factory; `Print()` takes Info as a parameter. |
| `pkg/version/cli/*` | Moved from `pkg/version/*` — embeds CLI's VERSION/COMMIT, exposes `Get`/`Cmd`/`Print`, `init()` registers with parent. |
| `pkg/version/daemon/*` | NEW — symmetric daemon counterpart. |
| `cmd/cli/commands/{root.go,common/run.go}` | Import alias `version "github.com/hashgraph/solo-weaver/pkg/version/cli"`. |
| `cmd/daemon/main.go` | Import alias `version "github.com/hashgraph/solo-weaver/pkg/version/daemon"`. |
| `.releaserc_cli.json` | NEW — CLI release config; tag prefix `solo-provisioner-v`, writes `pkg/version/cli/VERSION`, runs `task hash:cli:all` + `sign:cli:all`, 8 named assets. |
| `.releaserc_daemon.json` | NEW — daemon counterpart; tag prefix `solo-provisioner-daemon-v`, writes `pkg/version/daemon/VERSION`, 8 named assets. |
| `.releaserc` | DELETED. |
| `.github/workflows/flow-deploy-release-cli.yaml` | NEW — workflow_dispatch-only, runs `npx semantic-release --extends $(pwd)/.releaserc_cli.json`. |
| `.github/workflows/flow-deploy-release-daemon.yaml` | NEW — daemon counterpart. |
| `.github/workflows/flow-deploy-release-artifact.yaml` | DELETED. |
| `.gitignore` | Tracks the new VERSION/COMMIT paths under `pkg/version/{cli,daemon}/`. |
| `CLAUDE.md` | Notes both binaries' output paths, per-binary release pipelines, and the version-package shape. |
| `docs/claude/epics/00498-two-binary-build-layout.md` | Status `in progress` → `in review`; stories table updated; design constraints #3 + #4 updated for independent releases. |
| `docs/claude/plans/00516-taskfile-daemon-build-targets.md` | NEW combined plan. |
| `docs/claude/reviews/00516-taskfile-daemon-build-targets.md` | This file. |

## Review checklist

- [ ] **Aggregator wiring is symmetric.** Each top-level `:cli:all` call has a matching `:daemon:all` call right after it; `sources:`/`generates:` lists include both binaries' globs.
- [ ] **`deps` on per-binary aggregators are correct.** `hash:cli:all` deps `build:cli:all`; `sign:cli:all` deps `build:cli:all`; same for daemon. This is what lets the release configs call `task hash:cli:all` standalone without a separate build step.
- [ ] **No CLI/daemon cross-import in version packages.** `go list -deps ./cmd/cli` shows `pkg/version/cli` only (not daemon); `go list -deps ./cmd/daemon` shows `pkg/version/daemon` only.
- [ ] **Shared internal/ code is unchanged.** `internal/doctor`, `internal/state`, `internal/ui`, `internal/workflows` still import `pkg/version` (the parent), and `version.Get`/`Number`/`Commit` return the right binary's values via the init-registered current Info.
- [ ] **`.releaserc_*.json` are valid JSON and the asset paths exist** after `task hash:cli:all && task sign:cli:all` (CLI) and `task hash:daemon:all && task sign:daemon:all` (daemon) — each per-binary command's build dep fires automatically.
- [ ] **`tagFormat:` is set in each config** (`solo-provisioner-v${version}` and `solo-provisioner-daemon-v${version}`). semantic-release uses this to derive the next version from existing tags in that namespace.
- [ ] **VERSION write paths are per-binary.** `.releaserc_cli.json`'s `verifyRelease` writes `pkg/version/cli/VERSION`; daemon's writes `pkg/version/daemon/VERSION`.
- [ ] **Each workflow points at the right config.** `flow-deploy-release-cli.yaml` uses `--extends $(pwd)/.releaserc_cli.json`; daemon's uses `.releaserc_daemon.json`.
- [ ] **Each workflow reads the right VERSION file** in the "Retrieve Release Version" step.
- [ ] **The deleted `flow-deploy-release-artifact.yaml` has no remaining references** (e.g., from other workflow files or docs).
- [ ] **PR-check workflows are not broken.** `zxc-code-compiles.yaml` calls `task build` — now produces both binaries via aggregator wiring. `zxc-uat-test.yaml` still calls `task build:cli` directly — correct, UAT only exercises the CLI.

## Commands to run

```bash
cd /Users/bruno.marques/code/src/github.com/hashgraph/solo-weaver-wt/00516-taskfile-daemon-build-targets

# Lint
task lint:check

# Build, hash, sign each binary independently (release-style invocation)
rm -rf bin
task hash:cli:all                                 # build deps fire automatically
task hash:daemon:all
task sign:cli:all                                 # requires local GPG
task sign:daemon:all
ls bin/                                           # expect 4 binaries × {none, .sha256, .sha256.asc, .asc} = 16 files when fully signed

# Aggregated build (dev convenience)
rm -rf bin && task build
ls bin/                                           # expect 4 binaries

# Validate release configs
python3 -c "import json; [print(c, len(json.load(open(c))['plugins'][2][1]['assets']), 'assets') for c in ('.releaserc_cli.json', '.releaserc_daemon.json')]"

# Each release config's asset paths exist after hash + sign
python3 -c "
import json, os
for cfg in ('.releaserc_cli.json', '.releaserc_daemon.json'):
    assets = json.load(open(cfg))['plugins'][2][1]['assets']
    missing = [a['path'] for a in assets if not os.path.exists(a['path'])]
    print(cfg, '— missing:', missing if missing else 'none')
"

# go vet across affected packages
GOOS=linux GOARCH=amd64 go vet ./pkg/version/... ./cmd/cli/... ./cmd/daemon/... ./internal/...

# Each binary's version subpackage is isolated
GOOS=linux GOARCH=amd64 go list -deps ./cmd/cli   | grep "pkg/version"  # expect: pkg/version, pkg/version/cli (NOT daemon)
GOOS=linux GOARCH=amd64 go list -deps ./cmd/daemon | grep "pkg/version" # expect: pkg/version, pkg/version/daemon (NOT cli)
```

## Unit/integration tests

```bash
# macOS-safe smoke check (Linux-only packages aren't pulled in)
go test -tags='!integration' ./pkg/version/...

# Full unit suite (UTM VM, required for Linux-only deps)
task vm:test:unit

# Smoke integration in VM
task vm:test:integration TEST_NAME='^Test_StepKubeadm_Fresh_Integration$'
```

## Manual UAT

1. **Two binaries from one command.**

   ```bash
   rm -rf bin && task build
   ls bin/
   ```

   Expected:

   ```
   solo-provisioner-linux-amd64
   solo-provisioner-linux-arm64
   solo-provisioner-daemon-linux-amd64
   solo-provisioner-daemon-linux-arm64
   ```

2. **Each binary reports its own version** (independent VERSION files).

   Run these inside the UTM VM (or on any Linux host) — `solo-provisioner-linux-*` won't execute on macOS.

   **2a. Baseline: both binaries print their embedded version.**

   ```bash
   rm -rf bin && task build
   ./bin/solo-provisioner-linux-amd64 --version
   ./bin/solo-provisioner-daemon-linux-amd64 --version
   ```

   Expected: each binary prints JSON of the form `{"version":"0.0.0","commit":"<sha>","goversion":"go1.25.2"}` reading from `pkg/version/cli/VERSION` and `pkg/version/daemon/VERSION` respectively. The `commit` field is whatever `git rev-parse HEAD` returned at `go generate` time — same on both binaries (built from the same checkout).

   **2b. Format variants** (verify the shared `Info.Format` still works through both subpackages).

   ```bash
   ./bin/solo-provisioner-linux-amd64 --version --output yaml
   ./bin/solo-provisioner-daemon-linux-amd64 --version --output yaml
   ./bin/solo-provisioner-linux-amd64 version                  # subcommand form
   ./bin/solo-provisioner-daemon-linux-amd64 version
   ```

   Expected: YAML output from each; the `version` subcommand returns the same content as `--version`.

   **2c. Independence: bumping one VERSION must not affect the other.**

   ```bash
   echo -n "9.9.9" > pkg/version/daemon/VERSION
   task build:daemon GOOS=linux GOARCH=amd64
   ./bin/solo-provisioner-daemon-linux-amd64 --version              # expect version "9.9.9"
   ./bin/solo-provisioner-linux-amd64 --version                     # expect UNCHANGED — still reads pkg/version/cli/VERSION
   ```

   Expected: daemon reports `9.9.9`, CLI is unaffected. Revert the test edit when done:

   ```bash
   echo -n "0.0.0" > pkg/version/daemon/VERSION
   ```

   **2d. Shared-code reads the running binary's version** (proves the parent-package `SetCurrent` registration works through `internal/...` callers).

   ```bash
   # Force an error path so internal/doctor's Diagnose() runs and dumps version info.
   ./bin/solo-provisioner-linux-amd64 block node check --config /nonexistent.yaml 2>&1 | grep -A1 -i "version"
   ```

   Expected: the diagnostic output (powered by `internal/doctor/diagnose.go` calling `version.Number()` / `version.Commit()`) shows the CLI's version — proof that the parent package's runtime `Get()` returns the right binary's Info via the subpackage init() registration.

3. **Per-binary aggregator is self-sufficient.**

   ```bash
   rm -rf bin
   task hash:cli:all
   ls bin/                                                  # expect 2 CLI binaries + 2 .sha256 files; no daemon files
   ```

4. **Release config asset paths align with build outputs.**

   ```bash
   rm -rf bin
   task hash:cli:all && task hash:daemon:all              # build deps fire automatically
   python3 -c "
   import json, os
   for cfg in ('.releaserc_cli.json', '.releaserc_daemon.json'):
       print(cfg)
       assets = json.load(open(cfg))['plugins'][2][1]['assets']
       for a in assets:
           exists = '✓' if os.path.exists(a['path']) else '✗'
           print(f\"  {exists} {a['path']:60s} — {a['label']}\")
   "
   ```

   Expected: all 4 non-`.asc` paths for each binary show `✓` (the `.asc` signature files require `task sign:cli:all` / `task sign:daemon:all` with GPG configured).

## Release-workflow UAT (post-merge)

These steps verify both pipelines end-to-end without producing a real release. Each step is independent — you can dry-run the CLI workflow without ever touching the daemon one, or vice versa.

### A. Seed both tag namespaces (one-time, pre-first-release)

Both pipelines should stay in the `0.x.y` range (matching the CLI's existing lineage). Without seeding tags, semantic-release defaults to `1.0.0` on the first release — which we don't want.

- **CLI:** continue from the existing `v0.16.0` lineage so the next release is `0.16.x` / `0.17.0`.
- **Daemon:** start fresh at `0.0.0` so the first release is `0.0.1` or `0.1.0` (depending on commit kinds), well below `1.0.0`. The `releaseRules: [{ breaking: true, release: "minor" }]` block (already in both `.releaserc_*.json` files) overrides the default `BREAKING CHANGE → major` rule to keep both pipelines in `0.x.y` even when breaking commits appear.

```bash
git tag solo-provisioner-v0.16.0 v0.16.0
git tag solo-provisioner-daemon-v0.0.0 v0.16.0
git push origin solo-provisioner-v0.16.0 solo-provisioner-daemon-v0.0.0
```

(Tagging the daemon seed at `v0.16.0`'s commit aligns the two pipelines' baseline — each one analyzes the same set of commits as "candidates for the next release" on first run.)

Verify the seeding tags landed:

```bash
git fetch origin --tags
git tag -l 'solo-provisioner-v*'                    # expect: solo-provisioner-v0.16.0
git tag -l 'solo-provisioner-daemon-v*'             # expect: solo-provisioner-daemon-v0.0.0
```

### B. Dry-run each workflow

Both workflows accept `dry-run-enabled` as a `workflow_dispatch` input. Dry-run mode runs semantic-release with `--dry-run` so it computes the next version and lists what *would* happen, but does NOT push tags, write VERSION files, sign artifacts, or create a GitHub release.

**B1. CLI workflow dry-run:**

1. Go to **Actions → Deploy Release Artifact (CLI) → Run workflow**.
2. Select branch `main`; set `Perform Dry Run` to `true`; leave Go version default.
3. Trigger and watch the **Publish Semantic Release (CLI)** step.

What to verify in the log:

- A line like `The next release version is X.Y.Z` (e.g., `0.17.0` if there are unreleased feat commits since `v0.16.0`).
- The `::notice::Dry-run: Next CLI release would be solo-provisioner-vX.Y.Z` annotation surfaces in the workflow summary.
- No `git push` of a tag occurs.
- No release is created on the **Releases** page.

If the predicted version is `1.0.0`, the seeding tag (step A) didn't land — fix and retry.

**B2. Daemon workflow dry-run:**

Same as B1, but for **Deploy Release Artifact (Daemon)**. With the `solo-provisioner-daemon-v0.0.0` seeding tag in place, the predicted version on first run should be `0.0.1` (only fix commits since the seed) or `0.1.0` (any feat commit since the seed, including the daemon introduction itself). The annotation should read `solo-provisioner-daemon-v0.x.y`. If you see `v1.0.0`, the seeding tag (step A) didn't land — fix and retry.

### C. Verify the configs were honored

Inside each workflow run, expand the **Publish Semantic Release** step log and look for:

- `tagFormat` reported in semantic-release's verifyConditions output matches:
  - CLI: `solo-provisioner-v${version}`
  - Daemon: `solo-provisioner-daemon-v${version}`
- The list of plugins includes `@semantic-release/github` with the 8 named assets for that binary (full label strings should appear in the dry-run output).
- The `verifyRelease` exec lines show:
  - CLI: `printf "%s" "..." > pkg/version/cli/VERSION` and `task hash:cli:all` are queued (in dry-run they're listed but not executed — semantic-release skips exec hooks in `--dry-run`).
  - Daemon: same with `pkg/version/daemon/VERSION` and `task hash:daemon:all`.

Mismatch here means a typo in `.releaserc_cli.json` / `.releaserc_daemon.json` — fix in a follow-up PR.

### D. Verify isolation — running one workflow does not affect the other

After the CLI dry-run (B1) completes:

```bash
git fetch origin --tags
git tag -l 'solo-provisioner-v*'                    # unchanged: still just solo-provisioner-v0.16.0
git tag -l 'solo-provisioner-daemon-v*'             # unchanged: still empty
```

The dry-run must NOT have created any tags. Running the daemon dry-run after the CLI dry-run must also leave both tag lists unchanged.

### E. Real release (only when ready)

When you actually want to cut a release for one binary, repeat B1 or B2 with `Perform Dry Run = false`. Then verify:

```bash
# 1. The tag landed in the right namespace.
git fetch origin --tags
git tag -l 'solo-provisioner-v*'                    # CLI release: expect solo-provisioner-v<new-version>
git tag -l 'solo-provisioner-daemon-v*'             # daemon release: expect solo-provisioner-daemon-v<new-version>

# 2. The corresponding VERSION file was bumped on origin/main.
git show origin/main:pkg/version/cli/VERSION        # CLI release: shows the new version
git show origin/main:pkg/version/daemon/VERSION     # daemon release: shows the new version
# IMPORTANT: only the binary you released has its VERSION bumped — the other is untouched.

# 3. The GitHub release page shows 8 named assets with their labels.
gh release view "solo-provisioner-v<version>" -R hashgraph/solo-weaver       # CLI
gh release view "solo-provisioner-daemon-v<version>" -R hashgraph/solo-weaver # daemon
# Expected for each: 4 binary files + 4 signature/digest files, each labelled with "(linux/amd64)" or "(linux/arm64)".
```

Each release operates on a different tag namespace and writes a different VERSION file — the two pipelines never conflict even if triggered back-to-back.

### F. Release just one binary, leave the other alone

The core acceptance criterion for #517. Trigger only the **CLI** workflow (B1 / E). Then:

```bash
git fetch origin --tags
git tag -l 'solo-provisioner-v*'                    # one new tag
git tag -l 'solo-provisioner-daemon-v*'             # unchanged
git show origin/main:pkg/version/daemon/VERSION     # unchanged
```

Independent releases confirmed.

## Risks / rollback

- **Forgetting the seeding tag.** First CLI release would jump to `solo-provisioner-v1.0.0` instead of continuing from `v0.16.0`. Mitigated by the rollout checklist above and the dry-run mode each workflow supports.
- **Asset path typo in a `.releaserc_*.json`.** semantic-release fails at upload time. Mitigated by the python asset-existence check.
- **Cross-binary commit traffic.** Both pipelines compute the next version from every commit since the last tag in their namespace. Accepted limitation for V1; revisit with scope-based `releaseRules` if it causes spurious bumps.
- **Rollback.** `git revert` of the merge commit restores `.releaserc`, the original `flow-deploy-release-artifact.yaml`, and the single-file `pkg/version` layout. The seeding tag is harmless if left behind (no workflow references it post-revert) but can be deleted manually.
