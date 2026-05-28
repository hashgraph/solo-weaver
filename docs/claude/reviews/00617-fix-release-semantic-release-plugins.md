# 00617 — Fix release workflows missing semantic-release plugins

## Problem

The Daemon release workflow ([run 26542763251](https://github.com/hashgraph/solo-weaver/actions/runs/26542763251/job/78187847747)) failed with:

```
Error: Cannot find module '@semantic-release/commit-analyzer'
Require stack:
- /home/runner/_work/solo-weaver/solo-weaver/noop.js
```

Both `flow-deploy-release-daemon.yaml` and `flow-deploy-release-cli.yaml` (introduced in c5163a2 / #614) install only `semantic-release`, `@semantic-release/exec`, and `conventional-changelog-conventionalcommits` globally, but invoke semantic-release with `--extends "$(pwd)/.releaserc_*.json"`. With `--extends` anchored at `cwd`, semantic-release's plugin resolver walks up from a non-existent `noop.js` inside the project directory looking for `node_modules` — and never reaches the bundled plugins under the globally-installed `semantic-release` package. The previous (now-renamed) `flow-deploy-release-artifact.yaml` worked with the same install line because it did not use `--extends`.

## Solution

Install all four `@semantic-release/*` plugins referenced by `.releaserc_cli.json` and `.releaserc_daemon.json` explicitly, with versions pinned to the latest stable in the major required by `semantic-release@24.2.4`:

- `@semantic-release/commit-analyzer@13.0.1`
- `@semantic-release/release-notes-generator@14.1.1`
- `@semantic-release/github@11.0.6`
- `@semantic-release/exec@7.1.0` (already pinned)

## Changed files

| File | Change |
|------|--------|
| `.github/workflows/flow-deploy-release-daemon.yaml` | Expand `Install Semantic Release` to pin and install the three missing plugins. |
| `.github/workflows/flow-deploy-release-cli.yaml` | Same change, kept in lockstep with the daemon workflow. |

## Review checklist

- [ ] Plugin versions satisfy `semantic-release@24.2.4`'s peer/dep ranges (`commit-analyzer ^13`, `release-notes-generator ^14`, `github ^11`).
- [ ] Both release workflows have identical install steps (avoid drift between CLI and daemon pipelines).
- [ ] No other workflow invokes semantic-release with `--extends` and needs the same fix.
- [ ] Each plugin referenced by `.releaserc_cli.json` and `.releaserc_daemon.json` is installed globally.

## Manual verification

After merge to `main`, trigger the daemon release workflow with `dry-run-enabled: true` and confirm semantic-release reaches commit analysis (i.e. the previous `Cannot find module '@semantic-release/commit-analyzer'` error is gone). Repeat for the CLI workflow.

```bash
gh workflow run flow-deploy-release-daemon.yaml -R hashgraph/solo-weaver -f dry-run-enabled=true
gh workflow run flow-deploy-release-cli.yaml    -R hashgraph/solo-weaver -f dry-run-enabled=true
```

Expected: the install step prints all six packages, and the publish step logs either `The next release version is …` (dry-run) or `There are no relevant changes` — never the `MODULE_NOT_FOUND` traceback.

## Tests

None — workflow-only change, no Go code touched. `task lint` and `task test:unit` are unaffected.

## Closes

- #617
