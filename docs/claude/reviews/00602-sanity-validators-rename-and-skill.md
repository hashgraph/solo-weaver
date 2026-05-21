# PR Review Guide — refactor(sanity): Sanitize*/Validate* pattern + skill

**Issue:** [#602](https://github.com/hashgraph/solo-weaver/issues/602)  
**Branch:** `00602-sanity-validators-rename-and-skill` (stacked on `00600-allow-dot-in-username`)

---

## What Was Done

### Problem

`pkg/sanity` exposed ~10 input validators with inconsistent naming and overlapping behavior.
Half were `Validate*`-prefixed; the other half were bare nouns (`Identifier`, `Filename`,
`ModuleName`, `Username`) that sometimes stripped invalid characters and sometimes rejected
them — same naming style, opposite behavior. `Username` even returned `(string, error)`
like a sanitizer but actually rejected any input that would have been changed. This
mismatch is what produced issue #600 (SUDO_USER `nana.ec` rejected because `Username`
silently reused the strict identifier charset).

Additional confusion: `Filename` and `ModuleName` were literal aliases of `Identifier`,
so relaxing one would have broadened all three even though their domains have different
real-world rules.

### Solution

Split the API by behavior, let the prefix and signature carry the contract:

| Prefix | Signature | Behavior |
|---|---|---|
| `Sanitize*` | `(s string) (string, error)` | Strip invalid chars, return cleaned form |
| `Validate*` | `(s string) error` | Reject any input containing invalid chars |

Concrete renames:

- `Identifier` → `SanitizeIdentifier`
- `Filename` → `SanitizeFilename` (now a standalone function, no longer an alias)
- `ModuleName` → `SanitizeModuleName` (now a standalone function, no longer an alias)
- `Username` → `ValidateUsername` (returns `error` only; no more deceptive `(string, error)`)
- `ErrInvalidFilename` → `ErrInvalidName` (one shared sentinel across all sanitizers)

Character sets are unchanged in this PR — the only behavioral change is `Username`'s '.'
permissiveness, which already landed in #600 and is carried forward here because this
branch is stacked on it.

### Files Changed

| File | Change |
|------|--------|
| `pkg/sanity/sanity.go` | Rename `Identifier`/`Filename`/`ModuleName` → `Sanitize*` (standalone functions, no longer aliases); rename `Username` → `ValidateUsername` (return `error`); rename sentinel to `ErrInvalidName`; drop unused `filterValidUsernameChars` helper. |
| `pkg/sanity/sanity_test.go` | Rename test functions (`TestSanity_Filename` → `TestSanity_SanitizeFilename`, `TestSanity_Username` → `TestSanity_ValidateUsername`); drop `expected` fields from username cases (no longer returned); update two cases where the error message changed (`!!!` and `"   "` now report "contains invalid characters" rather than "contains no valid characters" because byte-iteration short-circuits before the empty-result branch). |
| `internal/kube/admin.go` | Switch `sanity.Username(sudoUser)` → `sanity.ValidateUsername(sudoUser)`; drop the unused `sanitizedUsername` local; use `sudoUser` directly in downstream `LookupUserByName` / log calls. |
| `pkg/kernel/module.go` | 3x `sanity.ModuleName(name)` → `sanity.SanitizeModuleName(name)`. |
| `.claude/skills/sanity-validators/SKILL.md` | New skill (directory form) documenting the Sanitize*/Validate* pattern, the decision matrix, and the rules for adding new validators. |

---

## Code Review Checklist

### `pkg/sanity/sanity.go`

- [ ] `SanitizeIdentifier`, `SanitizeFilename`, `SanitizeModuleName` are three **separate** functions even though their bodies are currently identical — no aliasing, no helper-of-helper.
- [ ] All three return `ErrInvalidName` on empty-after-filter.
- [ ] `ValidateUsername` returns `error` only.
- [ ] `ValidateUsername` runs checks in this order: empty → `..` substring → shell metachars → per-char loop.
- [ ] `isValidUsernameChar` permits `.` (carried from #600); `isValidIdentifierChar` does **not** permit `.` (still strict).
- [ ] No `Identifier`, `Filename`, `ModuleName`, or `Username` symbol remains exported.

### `pkg/sanity/sanity_test.go`

- [ ] All `ErrInvalidFilename` references are updated to `ErrInvalidName`.
- [ ] `TestSanity_ValidateUsername` calls `ValidateUsername(tc.input)` and asserts only on `err`.
- [ ] Existing security regression cases (`..`, shell metachars, control bytes, SQL/command injection) still produce errors.
- [ ] Positive dotted-username cases from #600 (`john.doe`, `a.b.c`, `.hidden`, `trailing.`, `nana.ec`) all pass.

### `internal/kube/admin.go`

- [ ] `ValidateUsername` is called **before** `LookupUserByName`. The original input (`sudoUser`) is passed downstream — there is no longer a `sanitizedUsername` local.
- [ ] Log messages and error wrappings reference `sudoUser` consistently.

### `pkg/kernel/module.go`

- [ ] All three call sites updated; the existing `sanitized != name` post-check at lines 130 / 143 still runs (`SanitizeModuleName` preserves the same return contract).

### `.claude/skills/sanity-validators/SKILL.md`

- [ ] Frontmatter `name:` matches the directory name (`sanity-validators`).
- [ ] `description:` is specific enough to match "validate this", "sanitize this", "check this username", or edits touching `pkg/sanity/sanity.go`.
- [ ] Decision matrix lists every public validator/sanitizer.
- [ ] History section references issues #600 and #602.

---

## Commands

### Lint

```bash
task lint:check
```

### Unit tests (macOS-compatible packages)

```bash
go test -race -cover -tags='!integration' \
  ./pkg/sanity/... \
  ./pkg/models/... \
  ./internal/kube/... \
  ./internal/ui/...
```

Expected: all packages pass; `pkg/sanity` coverage drops slightly (from ~75% to ~71%)
because the unused `filterValidUsernameChars` helper was removed.

### Full unit suite (run inside the UTM VM, includes `pkg/kernel/...`)

```bash
task vm:test:unit
```

---

## Manual UAT

1. Build for Linux and load into the VM (or a host with a dotted user account):

   ```bash
   task build:cli GOOS=linux GOARCH=amd64
   ```

2. Confirm the dotted-username happy path still works (regression on #600):

   ```bash
   sudo -u nana.ec ./bin/solo-provisioner-linux-amd64 kube cluster install --profile local
   ```

   Expected: `Initializing Kubernetes cluster` step succeeds; kubeconfig appears at
   `/home/nana.ec/.kube/config`.

3. Confirm kernel-module install still works (regression on the `SanitizeModuleName`
   rename):

   ```bash
   sudo ./bin/solo-provisioner-linux-amd64 kube cluster install --profile local
   lsmod | grep -E 'overlay|br_netfilter'
   ```

   Expected: both modules are listed.

4. Confirm the rejection paths still reject:

   ```bash
   sudo env SUDO_USER='ev..il' ./bin/solo-provisioner-linux-amd64 kube cluster install --profile local
   ```

   Expected: `invalid SUDO_USER environment variable: ev..il, cause: ... username contains path traversal sequences`.

---

## Out of scope

- Character-set divergence between `SanitizeFilename` / `SanitizeModuleName` /
  `SanitizeIdentifier` is intentionally **not** done in this PR — bodies are
  identical so the rename has zero behavior change for those sanitizers. The
  skill documents the future-divergence intent.
- Re-validation of persisted state on load (e.g. `BlockNodeState.ReleaseInfo.ChartRef`)
  is a separate concern that the skill flags but doesn't address here.
