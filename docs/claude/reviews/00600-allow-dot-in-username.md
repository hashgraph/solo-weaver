# PR Review Guide — fix(sanity): allow `.` in usernames

**Issue:** [#600](https://github.com/hashgraph/solo-weaver/issues/600)  
**Branch:** `00600-allow-dot-in-username`

---

## What Was Done

### Problem

`solo-provisioner` failed during `Initializing Kubernetes cluster` for any Linux user whose name
contains a `.` (e.g. `nana.ec`, `firstname.lastname`):

```
failed to configure kubeconfig: common.illegal_state: invalid SUDO_USER environment
variable: nana.ec, cause: common.illegal_argument: username contains invalid
characters: nana.ec
```

`sanity.Username()` only permitted `[A-Za-z0-9_-]`. Dotted usernames are legal Linux accounts
(NSS resolves them, `useradd` accepts them under the default `NAME_REGEX`), but our validator
rejected them. The validator is also reached via `SUDO_USER` inside
`internal/kube/admin.go:configureCurrentUserKubeConfig`, so any sudo-invoked CLI from such a
user died here.

### Solution

Relaxed **`Username()` only** to accept `.` in addition to alphanumerics, underscore, and hyphen.
The `..` path-traversal check and the shell-metacharacter check both run before the character
set check, so traversal attacks remain rejected. `Identifier()` / `Filename()` / `ModuleName()`
stay strict (no dots) because those are used for filenames and kernel-module names where the
stricter set is appropriate.

### Files Changed

| File | Change |
|------|--------|
| `pkg/sanity/sanity.go` | Added `isValidUsernameChar` / `filterValidUsernameChars` helpers (identifier set + `.`); `Username()` now uses them. Docstring updated. `Identifier`/`Filename`/`ModuleName` are unchanged. |
| `pkg/sanity/sanity_test.go` | Replaced the `john.doe` rejection case with five positive dotted-username cases (`john.doe`, `a.b.c`, `.hidden`, `trailing.`, `nana.ec`). Added a `Filename` regression case confirming `.` is still stripped from filenames. |
| `internal/kube/admin_test.go` | Converted the `security - SUDO_USER with period` rejection case into a `success - SUDO_USER with period (firstname.lastname)` case asserting the full happy path (lookup, mkdir, copy, chown). |

---

## Code Review Checklist

### `pkg/sanity/sanity.go`

- [ ] `isValidUsernameChar` returns true exactly when `isValidIdentifierChar` does **or** the byte is `.`.
- [ ] `Username()` still rejects `..` traversal (`strings.Contains(s, "..")` runs first).
- [ ] `Username()` still rejects shell metacharacters via `shellMetachars.MatchString(s)`.
- [ ] `Username()` still rejects empty input.
- [ ] `Username()` still enforces `sanitized == s` after filtering, so any non-username byte fails fast.
- [ ] `Identifier`, `ValidateIdentifier`, `Filename`, `ModuleName` are unchanged — dots remain forbidden in those contexts.

### `pkg/sanity/sanity_test.go`

- [ ] Positive cases cover: middle dot, multiple dots, leading dot, trailing dot, real-world `nana.ec`.
- [ ] Existing `..` traversal cases (`../john`, `john..doe`, `../../etc/passwd`) are still asserted as failures.
- [ ] Existing shell-metachar / control-char / SQL-injection / command-injection cases are still asserted as failures.
- [ ] `Filename` test asserts that `name.with.dots` becomes `namewithdots` (regression guard — confirms `Identifier` was not relaxed).

### `internal/kube/admin_test.go`

- [ ] The dotted-user case sets up full mocks (`LookupUserByName`, `CreateDirectory`, `CopyFile`, `WriteOwner`) and expects no error.
- [ ] The rest of the security cases (`/`, `\`, `,`, `:`, `;`, SQL/command injection, newline, control bytes) still expect rejection.

---

## Commands

### Lint

```bash
task lint:check
```

### Unit tests (macOS-compatible packages)

```bash
go test -race -cover -tags='!integration' ./pkg/sanity/... ./internal/kube/...
```

Expected: both packages pass; `pkg/sanity` reports the existing `~75%` coverage.

### Full unit suite (run inside the UTM VM)

```bash
task vm:test:unit
```

---

## Manual UAT

1. Build for Linux and copy into the VM (or a host with a dotted user):

   ```bash
   task build:cli GOOS=linux GOARCH=amd64
   ```

2. As a user named `nana.ec` (or any `firstname.lastname` POSIX account):

   ```bash
   sudo ./bin/solo-provisioner-linux-amd64 kube cluster install --profile local
   ```

   Expected: the `Initializing Kubernetes cluster` step completes without the previous
   `invalid SUDO_USER environment variable: nana.ec` error.

3. Verify kubeconfig was installed into the user's home directory:

   ```bash
   ls -l /home/nana.ec/.kube/config
   ```

   Expected output:

   ```
   -rw------- 1 nana.ec nana.ec ... /home/nana.ec/.kube/config
   ```

4. (Negative) Confirm a malicious `SUDO_USER` is still rejected:

   ```bash
   sudo env SUDO_USER='ev..il' ./bin/solo-provisioner-linux-amd64 kube cluster install --profile local
   ```

   Expected: `invalid SUDO_USER environment variable: ev..il, cause: common.illegal_argument: username contains path traversal sequences`.

---

## Out of scope

- The block-node install error reported in the same session (`non-absolute URLs should be in form of
  repo_name/path_to_chart, got: block-node-server`) is a separate issue — see the resolver
  notes from that triage. Not addressed here.
- `chartRefResolver` treating non-empty `StrategyState` as authoritative without re-validating the
  persisted value is a latent concern worth a separate issue.
