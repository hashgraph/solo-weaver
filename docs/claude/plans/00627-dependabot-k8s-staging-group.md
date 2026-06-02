# #627 — Group and upgrade k8s.io staging modules together via Dependabot

> **Issue:** https://github.com/hashgraph/solo-weaver/issues/627
> **Story branch:** `00627-dependabot-k8s-staging-group`
> **PR base:** `main` (branched from `origin/main` @ `3866a9c`)
> **PR closes:** #627
> **Supersedes:** Dependabot PRs #491 (client-go 0.36.1) and #493 (apimachinery 0.36.1) — close after this lands.

## Summary

The eight `k8s.io/*` staging modules we depend on must always move together to the same minor version, but `.github/dependabot.yml` currently has no `groups:` block under the `gomod` ecosystem, so Dependabot opens one PR per module. This PR (1) adds a `kubernetes` group to the gomod ecosystem covering all eight staging modules and (2) as a one-off, upgrades all eight to a single consistent minor — `v0.36.1` (latest at time of writing) — clearing the existing `v0.35.3` / `v0.35.2` intra-release drift.

## Problem

Today in `go.mod`:

| Module | Current | Why pinned here |
|---|---|---|
| `k8s.io/apimachinery` | `v0.35.3` | direct |
| `k8s.io/client-go` | `v0.35.3` | direct |
| `k8s.io/api` | `v0.35.3` | indirect |
| `k8s.io/apiextensions-apiserver` | `v0.35.2` | indirect |
| `k8s.io/apiserver` | `v0.35.2` | indirect |
| `k8s.io/cli-runtime` | `v0.35.2` | indirect |
| `k8s.io/component-base` | `v0.35.2` | indirect |
| `k8s.io/kubectl` | `v0.35.2` | indirect |

The `v0.35.3` vs `v0.35.2` drift is already visible — confirms the symptom from the issue. Open Dependabot PRs #491 and #493 are trying to drag `client-go` and `apimachinery` to `v0.36.1` individually; both will fail CI in isolation because of the staging-set coupling.

## Decisions

| Question | Decision |
|---|---|
| Target version for the one-off upgrade? | `v0.36.1` (latest at time of writing; matches what Dependabot PRs #491 and #493 already proposed). |
| Which modules belong in the `kubernetes` group? | Exactly the eight modules listed in the issue body: `api`, `apimachinery`, `client-go`, `apiextensions-apiserver`, `apiserver`, `cli-runtime`, `component-base`, `kubectl`. |
| What about `k8s.io/klog/v2`, `k8s.io/kube-openapi`, `k8s.io/utils`, `sigs.k8s.io/*`? | Out of scope — independent release cadence. Stay on Dependabot's default per-module flow. |
| Close PRs #491 / #493 manually or let this PR's branch land first? | Close them manually after this PR merges, referencing #627. |
| Use `update-types` to restrict the group to minor/patch only? | No — staging modules don't release independent majors. Default (all update types) matches the issue's spec verbatim. |

## Scope

### `.github/dependabot.yml`
- [ ] Add a `groups:` block under the existing `gomod` ecosystem entry with key `kubernetes` and the eight `k8s.io/*` patterns from the issue.

### Dependency upgrade
- [ ] Bump all eight `k8s.io/*` staging modules to `v0.36.1` in `go.mod`.
- [ ] `go mod tidy` to refresh `go.sum`.
- [ ] `go mod vendor` to refresh `vendor/`.
- [ ] Audit transitive callers that touch `k8s.io/api`, `apimachinery`, `client-go` typed APIs (mainly `internal/kube/`, `internal/bll/cluster/`, `internal/blocknode/`) for v0.36 breakages — k8s minor bumps typically remove deprecated APIs.

### Verification
- [ ] `task lint:check` passes.
- [ ] `task build` produces both binaries on `darwin/arm64` (local) and `linux/amd64` (cross-compile).
- [ ] `task test:unit` passes locally for non-Linux-only packages.
- [ ] `task vm:test:unit` passes (full coverage including Linux-only packages).
- [ ] `task license:check` passes.

## Out of scope

- Grouping or upgrading `k8s.io/klog/v2`, `k8s.io/kube-openapi`, `k8s.io/utils`, or any `sigs.k8s.io/*` module — independent cadence, per issue.
- Closing the pre-existing Dependabot PRs #491 and #493 — that's a manual follow-up by the user after merge; this PR's body should reference them so the cleanup is visible.
- Refactoring any Kubernetes client code beyond what `v0.36.1` compile/typecheck breakage forces.
- Integration tests (`task vm:test:integration`) — out of scope unless unit tests surface something that warrants integration coverage.

## Test plan

- [ ] Unit: `task test:unit` (macOS) and `task vm:test:unit` (UTM VM, Linux-only coverage).
- [ ] Build: `task build` — cross-compile both binaries to confirm no `v0.36.1`-related compile breakage on either target.
- [ ] Lint/format: `task lint:check`.
- [ ] License headers: `task license:check`.
- [ ] Manual verify of Dependabot config syntax: `cat .github/dependabot.yml` is well-formed; once merged, watch the next Dependabot run to confirm grouped PRs appear (out-of-band — happens after merge).

## Risks / rollbacks

- **Risk:** `v0.36.x` removes or renames a typed API used in `internal/kube/` or `internal/bll/cluster/`, surfacing as a compile error. Mitigation: surface during the build step before opening the PR; address per-call-site or roll the target back to `v0.36.0`.
- **Risk:** Vendor diff is large (hundreds of files under `vendor/k8s.io/...`). This is expected for a k8s minor bump — flag in the PR body so reviewers know to skim, not line-read, the vendor delta.
- **Rollback:** Revert the single commit (or three commits if split: dependabot.yml, go.mod/go.sum, vendor/). No runtime state to undo.
