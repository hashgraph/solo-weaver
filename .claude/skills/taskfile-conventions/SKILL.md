---
name: taskfile-conventions
description: Use this skill when adding, restructuring, or extracting tasks in this repo's Task (taskfile.dev) configuration — e.g. "add a new build target", "split Taskfile.yaml", "add tasks for X", "where should this task live?", any edit that touches Taskfile.yaml or taskfiles/*.yaml. Captures the repo's conventions for when to inline vs. extract to a sub-Taskfile, the family-naming pattern (build:X / hash:X:* / sign:X:*), the OS/arch fan-out structure, and the `includes` + `flatten: true` discovery model. Read this BEFORE adding a new task family or extracting an inline block.
---

# Taskfile conventions for solo-weaver

The repo uses [Task](https://taskfile.dev) as its build system. Tasks are split across:

- `Taskfile.yaml` (root) — top-level aggregators (`build`, `hash`, `sign`, `clean`, `generate`, `lint:check`, `license:check`, `vendor`, `mocks`, `run`, etc.) plus shared `vars:` and `includes:` wiring.
- `taskfiles/*.yaml` — topic-scoped sub-Taskfiles, included into the root via `includes:` with `flatten: true`.

This skill codifies the conventions so the structure stays coherent as the repo grows.

## When to inline vs. extract

**Inline in `Taskfile.yaml`** when:

- The task is one of the cross-cutting aggregators (`build`, `hash`, `sign`, `clean`, `generate`, `lint*`, `license*`, `vendor`, `mocks`).
- The task is a one-off / utility that doesn't belong to a family (e.g. `build:image`).
- The block is under ~30 lines and has no internal helpers.

**Extract to `taskfiles/<topic>.yaml`** when any of the following applies:

- The task family is **topic-scoped** (one binary, one infrastructure component, one test environment, one external integration). Existing topics: `cli`, `daemon`, `proxy`, `alloy`, `teleport`, `tests`, `uat`, `vm`.
- The family has more than 4 tasks **or** more than ~50 lines, especially with the OS/arch fan-out pattern below.
- The family is logically self-contained: removing it from the root doesn't break unrelated tasks.
- A new "sister" family is being introduced alongside an existing extracted one (e.g. adding `daemon` when `cli` is already extracted — keep them symmetric, not one inline + one extracted).

If you're adding a new binary's build/hash/sign family, **always extract**. That's what `taskfiles/cli.yaml` and `taskfiles/daemon.yaml` are for.

## Sub-Taskfile structure

Every file under `taskfiles/` follows this template:

```yaml
# SPDX-License-Identifier: Apache-2.0

version: 3

tasks:
  <task-name>:
    desc: "..."
    cmds:
      - ...
```

- SPDX header is required (enforced by `task license:check`).
- `version: 3` matches the root Taskfile.
- No `vars:` block needed at the top — the root Taskfile's `vars:` (e.g. `OS`, `ARCH`, `LDFLAGS`, `GO_VERSION`) are inherited automatically via the `flatten: true` include in the root.
- Do **not** declare `includes:` inside a sub-Taskfile; only the root Taskfile manages cross-file wiring.

## Registering a sub-Taskfile

Add an entry under `includes:` in the root `Taskfile.yaml`:

```yaml
includes:
  <topic>:
    taskfile: ./taskfiles/<topic>.yaml
    flatten: true
```

`flatten: true` is the repo convention. It pulls the sub-Taskfile's tasks into the root namespace as-is — no `<topic>:<task>` prefix, so `build:cli` stays `build:cli` (not `cli:build:cli`). This is why the family-naming pattern below carries the topic in the task name itself.

## Family-naming pattern (build / hash / sign)

For per-binary or per-artifact families, use the four-task structure with the topic embedded in the name:

```
<verb>:<topic>:all          → user-facing entry that builds the whole matrix
<verb>:<topic>:all:os       → internal; for-each over OS, fans out to :all:arch
<verb>:<topic>:all:arch     → internal; for-each over ARCH, fans out to the leaf
<verb>:<topic>              → user-facing leaf; builds one (OS, ARCH) combination
```

Verbs in use today: `build`, `hash`, `sign`. The pattern is taken from `taskfiles/cli.yaml` and `taskfiles/daemon.yaml` — copy from one of those when adding a new binary's family.

Naming rules:

- The two internal levels are marked `internal: true` so they don't show in `task --list`.
- Topic is **always** the second segment, even when nesting deeper: `build:cli:all`, not `cli:build:all`.
- The leaf task (`build:<topic>`) reads `GOOS`/`GOARCH` via `{{coalesce .GOOS OS}}` / `{{coalesce .GOARCH ARCH}}` so it can be invoked directly with explicit vars (e.g. `task build:cli GOOS=linux GOARCH=amd64`) or driven by the `:all` fan-out.
- The leaf task chmods the produced binary (`chmod +x bin/...`) — Task on macOS preserves the build-time perms but CI runners sometimes drop them.

Hash and sign families follow the same shape; `sign:<topic>` typically calls `hash:<topic>` first, then GPG-signs both the binary and the `.sha256`.

## Top-level aggregators (`build`, `hash`, `sign`)

The root Taskfile's `build`, `hash`, `sign` tasks are **the entry points CI uses**. They are deliberately shallow:

```yaml
build:
  deps: [ "vendor" ]
  cmds:
    - task: "clean"
    - task: "generate"
    - task: "build:cli:all"   ← retargets when a binary becomes the new default

hash:
  deps: [ "build" ]
  cmds:
    - task: "hash:cli:all"

sign:
  deps: [ "build" ]
  cmds:
    - task: "sign:cli:all"
```

The aggregators currently build / hash / sign **only the CLI** because:

- `task build` runs in CI; only what's listed here ends up in `bin/*` for the release pipeline.
- Wiring additional binaries (daemon, etc.) into the aggregators is an explicit decision that affects release artifacts — make it a separate, named PR (e.g. epic #498's story #516).

Daemon-style "side" binaries are built via their explicit `task build:<topic>` entry point until they're deliberately promoted into the aggregator.

## OS/arch matrix

The root Taskfile declares:

```yaml
vars:
  OS: [ linux ]
  ARCH: [ amd64, arm64 ]
```

That's what the `:all:os` and `:all:arch` internal tasks iterate over. **Adding a platform** (e.g. `darwin`) means updating the root `vars:` once; every extracted task family picks it up automatically via the `for: var: OS` / `for: var: ARCH` constructs.

For a Linux-only binary, the leaf task can hardcode `GOOS: linux` and skip the `:all:os` level — but prefer following the matrix pattern unless you have a concrete reason.

## Common pitfalls

- **Forgetting `internal: true`** on the `:all:os` / `:all:arch` rungs. They show up in `task --list` and confuse anyone running `task` for the first time.
- **Putting `includes:` in a sub-Taskfile.** Only the root manages includes; sub-files describing their own includes silently fail to pick up cross-file deps because Task resolves them per-root.
- **Hardcoding output paths.** Use `bin/<binary-name>-{{coalesce .GOOS OS}}-{{coalesce .GOARCH ARCH}}` so the binary path stays consistent across direct invocations and `:all` fan-outs.
- **Naming the topic with a binary's old name.** When a binary gets renamed, rename its task family at the same time (rename happens in lock-step with the cmd/ rename, not in a follow-up). Stale task names confuse the next reader.
- **Skipping the SPDX header.** `task license:check` will fail on the next CI run; pre-commit hooks won't catch it.

## When updating: also check

When you add, rename, or extract a task family, also grep for references that need to follow:

```bash
grep -rn "<old-task-name>" \
  .github/workflows/ \
  CLAUDE.md \
  docs/ \
  taskfiles/ \
  Taskfile.yaml \
  2>/dev/null
```

Common places that reference task names directly:

- `.github/workflows/*.yaml` — CI invokes `task <name>` directly.
- `CLAUDE.md` — developer reference; examples of `task build:cli ...` etc.
- `docs/dev/*.md` — UAT and dev-loop docs.

A rename that leaves any of these stale will silently break someone's flow.
