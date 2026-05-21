---
name: sanity-validators
description: Use this skill BEFORE writing or modifying any input validation in this repo — e.g. "validate this CLI flag", "sanitize this filename", "check this username", "add a new sanity helper", any edit that touches pkg/sanity/sanity.go or any caller that runs user-supplied / env-derived / config-derived strings through a `sanity.*` helper. Captures the Sanitize* vs Validate* naming pattern, the per-validator character sets, and the decision matrix for picking the right helper. Read this BEFORE relaxing or tightening any validator — the rules are scoped and the wrong helper is what produced issue #600.
---

# pkg/sanity validators — naming, scope, and decision matrix

`pkg/sanity` is the single chokepoint for validating untrusted strings before they
reach the OS, the shell, Helm, kubectl, file paths, or NSS lookups. Every helper
in this package follows one of two patterns; using the wrong one (or reusing a
helper scoped to a different domain) is how validation bugs slip in.

---

## The two patterns

| Prefix | Signature | Behavior |
|---|---|---|
| `Sanitize*` | `(s string) (string, error)` | **Strips** invalid characters and returns the cleaned form. Errors only if nothing valid remains (returns `ErrInvalidName`). The caller is expected to use the returned value, not the input. |
| `Validate*` | `(s string) error` | **Rejects** any input containing invalid characters. The caller uses the input string as-is when the error is `nil`. |

**Signature is destiny.** A `(string, error)` return means "I might rewrite your
input" — if the caller doesn't use the returned value, the validation is wasted
or wrong. An `error` return means "I won't touch it" — the input is safe to use.

**Default to `Validate*` for anything sourced from a user or environment.** Use
`Sanitize*` only when the codebase itself is constructing a string from
trusted-but-messy inputs (e.g. deriving a filename from a label).

---

## Function catalog

### Sanitizers (return `(string, error)`)

| Function | Charset | Use for |
|---|---|---|
| `SanitizeIdentifier` | `[A-Za-z0-9_-]` | Generic identifiers when no more specific helper fits. |
| `SanitizeFilename` | `[A-Za-z0-9_-]` (today, same as identifier) | Derived filenames. Standalone so future relaxation (e.g. permit `.`) can land without touching identifier or module-name callers. |
| `SanitizeModuleName` | `[A-Za-z0-9_-]` (today, same as identifier) | Linux kernel module names. Standalone so future tightening (drop `-`, since kernel modules use `_`) can land without touching identifier or filename callers. |
| `SanitizePath` | path semantics | File-system paths. Cleans, makes absolute, rejects `..` segments and shell metacharacters. |
| `Alphanumeric` | `[A-Za-z0-9]` | Pure transformer (`string` return only, no error). For display-string normalisation, not security validation. |

All sanitizers return `ErrInvalidName` when filtering leaves nothing.

### Validators (return `error`)

| Function | Charset / contract | Use for |
|---|---|---|
| `ValidateIdentifier` | `[A-Za-z0-9_-]`, non-empty | Namespace / release / profile names; anything that flows into Kubernetes object names. |
| `ValidateUsername` | `[A-Za-z0-9_.-]`, non-empty, no `..`, no shell metachars | POSIX/Linux usernames (`SUDO_USER`, owner accounts). Permits `.` for `firstname.lastname` accounts. |
| `ValidateDNSName` | `[A-Za-z0-9.-]` matching RFC 952/1123 host pattern | Hostnames, FQDNs, cluster names derived from DNS. |
| `ValidateHexToken` | `[0-9a-fA-F]`, length ≤ 4096 | Teleport join tokens and similar hex secrets. |
| `ValidateHostPort` | hostname or hostname:port; no path components, no shell metachars | `host:port` style endpoints. Use `ValidateURL` for full URLs. |
| `ValidateChartReference` | OCI URL / classic `repo/chart` / local path | Helm chart references. **Critical** for the block-node `Chart` field. |
| `ValidateInputFile` | path semantics + file exists + within allowed roots | User-supplied file paths from CLI flags. |

---

## Decision matrix — "which one do I use for X?"

Match by what the string represents in the real world, not by what it looks like:

- **Kubernetes namespace / release / profile name** → `ValidateIdentifier`
- **Linux user account** (SUDO_USER, file owner, sudoers principal) → `ValidateUsername`
- **Kernel module name** → `SanitizeModuleName` (current callers want the sanitized form for the modprobe call)
- **Helm chart reference** → `ValidateChartReference`
- **Hostname / FQDN / cluster DNS name** → `ValidateDNSName`
- **host:port endpoint** → `ValidateHostPort`
- **Hex token / shared secret** → `ValidateHexToken`
- **Filesystem path from a user** → `SanitizePath` (when you'll use the cleaned form) or `ValidateInputFile` (when you need the file to exist and be in an allowed root)
- **Derived filename from an internal label** → `SanitizeFilename`
- **Display-only string normalisation** → `Alphanumeric`
- **Nothing on this list matches** → do **not** repurpose a near-fit; add a new validator scoped to the actual domain.

---

## Hard rules

### 1. Do not alias one validator to another

Before issue #602, `Filename` and `ModuleName` were literal aliases of `Identifier`.
That made it impossible to relax the username charset (issue #600) without
implicitly broadening filenames and kernel-module names too. Each domain gets its
own function even when the charset is currently identical — future divergence
should not require touching unrelated callers.

### 2. Validate at the boundary, not in the middle

The right call site is **the first point a string enters the program from an
untrusted source**:

- CLI flag parsing in `cmd/cli/commands/**/init.go`
- Config-file unmarshal in `pkg/config/global.go`
- Env-var reads (e.g. `os.Getenv("SUDO_USER")` in `internal/kube/admin.go`)
- Public API entry points

Do not assume an upstream caller already validated. If you're about to use a
string in a Helm/kubectl/shell/path/NSS operation and the validator hasn't run
in the current function or its immediate caller, run it.

### 3. Don't reuse `Validate*` against persisted state

Validators are designed for fresh user input. When code loads a previously
persisted value (e.g. `BlockNodeState.ReleaseInfo.ChartRef` from disk) and the
value will be used as authoritative without re-validation, the validator's
guarantees no longer hold — a corrupted or pre-relaxation-era state file can
smuggle in values that the current validator would reject. If you add new
persisted fields, also add a validation pass at the load boundary.

### 4. The `..` substring check runs **before** the per-char check

`ValidateUsername` (and any future validator that permits `.`) rejects `..` as
a substring before iterating characters. Don't reorder these.

### 5. Shell metachars are a strict superset of what the charset rejects

`shellMetachars = [;&|$\x60<>(){}[\]*?~]` is intentionally a separate gate so
the error message can distinguish "contains shell metacharacter" from "contains
invalid character". Both produce safe behavior; only the error text differs. Do
not collapse them into a single regex unless you also change the error
messages.

---

## When adding a new validator

1. **Name it for the domain, not the charset.** `ValidateUsername`, not
   `ValidateAlnumDotUnderscoreHyphen`.
2. **Pick the right prefix**: `Validate*` (strict) if any non-empty caller will
   pass an env/CLI/config string; `Sanitize*` only if every call site wants the
   cleaned form.
3. **Don't reuse `isValidIdentifierChar` if your domain might diverge.** Add a
   domain-specific predicate (`isValidUsernameChar`, `isValidFooBarChar`) even
   if it returns the same set today.
4. **Reject empty first.** Every validator should return `"<name> cannot be empty"`
   on `""`.
5. **Reject `..` and shell metachars next**, before the per-char loop, when the
   permitted charset includes any of `. / -`.
6. **Add tests for**: empty input, valid happy path, every shell metachar, every
   control byte (`\x00`, `\n`, `\r`, `\t`, `\x07`, `\x1b`), a `..` traversal
   attempt, a SQL/command-injection attempt, and a path-traversal attempt. Mirror
   the structure of `TestSanity_ValidateUsername` in `pkg/sanity/sanity_test.go`.
7. **Update this skill** with the new validator and its decision-matrix row.

---

## History (so the conventions don't drift)

- **Issue #600**: `Username` rejected `firstname.lastname` accounts (e.g.
  `nana.ec`). Fixed by permitting `.` in the username charset and reaffirming
  the existing `..` traversal check.
- **Issue #602**: split the package into `Sanitize*` vs `Validate*` by behavior;
  dropped the `Filename`/`ModuleName`-as-aliases-of-`Identifier` shortcut so each
  domain can evolve independently; renamed `Username` → `ValidateUsername` to
  match its actual semantics (returns `error`, not a sanitized form).
