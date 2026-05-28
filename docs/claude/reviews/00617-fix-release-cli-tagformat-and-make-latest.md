# Review guide — release-pipeline follow-up fixes (refs #617)

## Summary

Follow-up to #618 (which fixed plugin loading). Two issues surfaced once the
two new release workflows were exercised end-to-end:

1. **CLI tag namespace jumped to `1.0.0`.** `solo-provisioner-v${version}` had no
   prior tags in that namespace, so semantic-release fell back to its hardcoded
   first-release default of `1.0.0` and produced `solo-provisioner-v1.0.0-rc.1`
   instead of continuing the existing `v0.17.0 → v0.18.0` lineage. Reverting the
   CLI `tagFormat` to `v${version}` lets semantic-release see the existing
   `v0.17.0` / `v0.17.0-rc.2` tags as prior releases and compute the next bump
   normally. As a bonus, the existing `refs/tags/v*` tag-protection ruleset
   automatically re-covers CLI tags.

2. **Daemon release claimed the homepage "Latest" badge.** Once
   `solo-provisioner-daemon-v0.1.0` was published, GitHub auto-promoted it to
   "Latest" by recency, kicking the CLI's `v0.17.0` out of the homepage sidebar.
   The daemon is internal infrastructure — the user-facing artifact is the CLI.
   Fixed by adding a `success` hook to `.releaserc_daemon.json` that calls
   `gh release edit ... --latest=false` after publication.

While here, the daemon `tagFormat` was also shortened from
`solo-provisioner-daemon-v${version}` to `daemon-v${version}` so the release
list reads more cleanly.

## Files changed

| File | Change |
|---|---|
| `.releaserc_cli.json` | `tagFormat`: `solo-provisioner-v${version}` → `v${version}`. Restores the pre-#614 tag namespace so semantic-release sees `v0.17.0` lineage. |
| `.releaserc_daemon.json` | `tagFormat`: `solo-provisioner-daemon-v${version}` → `daemon-v${version}`. Added `success` step that runs `gh release edit ${nextRelease.gitTag} --repo "$GITHUB_REPOSITORY" --latest=false`. |
| `.github/workflows/flow-deploy-release-cli.yaml` | Dry-run echo updated to print `v${VERSION}` instead of `solo-provisioner-v${VERSION}`. |
| `.github/workflows/flow-deploy-release-daemon.yaml` | Dry-run echo updated to print `daemon-v${VERSION}` instead of `solo-provisioner-daemon-v${VERSION}`. |
| `CLAUDE.md` | Updated the tag-namespace bullet under "Key Conventions" to match new formats and document the daemon `make_latest=false` post-publish step. |

## Review checklist

- [ ] `.releaserc_cli.json`: `tagFormat` is `v${version}` (single line).
- [ ] `.releaserc_daemon.json`: `tagFormat` is `daemon-v${version}`; new `success` array contains exactly the one `@semantic-release/exec` call.
- [ ] The daemon `success` cmd quotes `"$GITHUB_REPOSITORY"` (shell expansion at runtime, not lodash-template-time) and uses `${nextRelease.gitTag}` (templated by semantic-release/exec before shell sees it).
- [ ] Dry-run echo messages in both workflows match the new formats.
- [ ] `CLAUDE.md:239` reflects both new namespaces and the `make_latest=false` note.
- [ ] Historical plan/epic docs under `docs/claude/plans/00516-*.md` and `docs/claude/epics/00498-*.md` are intentionally **not** rewritten — they describe what was planned at the time and remain accurate as a record.

## Test commands

```bash
# Sanity-check the two releaserc files are valid JSON
jq . .releaserc_cli.json .releaserc_daemon.json

# Lint (no Go was touched, but the project rule says to run lint after every change)
task lint

# License-header check
task license:check
```

There are no unit or integration tests that exercise the release configs directly. End-to-end validation is via the workflow runs below.

## Manual UAT

### After merge

1. **Delete the dangling tags + releases left over from the failed bootstrap runs.**
   The CLI `solo-provisioner-v1.0.0-rc.1` and the daemon's
   `solo-provisioner-daemon-v0.0.0` seed and `solo-provisioner-daemon-v0.1.0`
   release all live in namespaces that the new configs no longer consult, but
   they should be cleaned up to avoid noise on the Releases page.

   ```bash
   # CLI 1.0.0-rc.1 (release + tag)
   gh release delete solo-provisioner-v1.0.0-rc.1 --repo hashgraph/solo-weaver --yes
   git push --delete origin solo-provisioner-v1.0.0-rc.1

   # Daemon 0.1.0 (release + tag)
   gh release delete solo-provisioner-daemon-v0.1.0 --repo hashgraph/solo-weaver --yes
   git push --delete origin solo-provisioner-daemon-v0.1.0

   # Daemon seed tag (no release attached)
   git push --delete origin solo-provisioner-daemon-v0.0.0
   ```

2. **Plant a daemon seed tag in the new namespace** so the first daemon run
   produces `daemon-v0.1.0` instead of `daemon-v1.0.0`. Use the parent of the
   daemon-introducing commit so the auto-generated release notes scope cleanly:

   ```bash
   git tag daemon-v0.0.0 8a562a7   # parent of b294657 feat(cmd): two-binary build layout
   git push origin daemon-v0.0.0
   ```

3. **Re-trigger `Deploy Release Artifact (CLI)` on `rc/purge-storage`.**
   Expected:

   ```
   [semantic-release] › ℹ  The next release version is 0.18.0-rc.1
   …
   [semantic-release] › ✔  Created tag v0.18.0-rc.1
   ```

   Verify on GitHub: a new `Pre-release` `v0.18.0-rc.1` appears.
   The repo home "Latest" badge should still read `v0.17.0` (CLI's last stable),
   not `daemon-v0.1.0`.

4. **Re-trigger `Deploy Release Artifact (Daemon)` on `main`.** Expected:

   ```
   [semantic-release] › ℹ  The next release version is 0.1.0
   …
   [semantic-release] › ✔  Created tag daemon-v0.1.0
   …
   [semantic-release] [@semantic-release/exec] › ℹ  Call script gh release edit daemon-v0.1.0 --repo "$GITHUB_REPOSITORY" --latest=false
   ```

   Verify on GitHub: `daemon-v0.1.0` exists as a regular release (not
   pre-release) but does **not** carry the `Latest` chip; that chip stays on
   `v0.17.0` until the next CLI stable.

   ```bash
   gh release view daemon-v0.1.0 --repo hashgraph/solo-weaver \
     --json tagName,isPrerelease | jq .
   # Cross-check via API that make_latest is no longer "true":
   gh api repos/hashgraph/solo-weaver/releases/tags/daemon-v0.1.0 \
     --jq '{tag: .tag_name, draft: .draft, prerelease: .prerelease, make_latest: .make_latest}'
   # Expected: make_latest = "false"
   ```

5. **(Optional follow-up, not in this PR)** The `refs/tags/v*` ruleset now
   covers CLI tags again, but the new `daemon-v*` namespace is unprotected.
   Open a follow-up to add a sibling ruleset entry covering `refs/tags/daemon-v*`
   with the same `non_fast_forward` / `deletion` / `update` /
   `required_signatures` rules.
