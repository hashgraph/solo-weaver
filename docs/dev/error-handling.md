# Error Handling

solo-weaver builds errors with [`errorx`](https://github.com/joomcode/errorx) and
decorates the ones that cross a package boundary with
[`errx`](https://github.com/automa-saga/errx). `errorx` answers *what kind of
error is this*; `errx` answers *why it failed and what the operator should do*.

`errx` stores its data as `errorx` properties and traverses `errorx` cause chains,
so nothing about error *construction* changed: namespace selection still lives in
`.claude/skills/errorx-conventions/SKILL.md`, and `errors.New` / `fmt.Errorf`
remain forbidden in production code by `forbidigo`.

---

## Design principles

The one-screen summary every migration PR follows; the sections below carry the
rationale and the mechanics.

**Construction**

- `errx` carries reasons + operator hints; `errorx` stays the construction layer.
  `errorx` answers *what kind of error*, `errx` answers *why and what to do*.
- No bare `fmt.Errorf` / `errors.New` at package boundaries (`forbidigo`-enforced).
- No new `errorx` namespaces or typed errors — built-in `errorx.<Type>` plus a
  reason code. A fresh namespace silently changes the operator-visible error code.
- A typed error is earned only when a caller must branch on that exact error —
  then `errorx.<Builtin>.NewSubtype("…")` in that package's `errors.go`.
- Already-namespaced packages stay as they are — reasons/hints are orthogonal to
  renaming types.

**Decoration**

- Decorate only where the error leaves the package (exported return,
  `StepFailureReport`, `RunE`); inner layers keep a plain `errorx.<Type>.Wrap` —
  the outermost reason wins, so stacked decoration duplicates hints and
  overwrites reasons.
- Hints only for actionable failures (config/disk/RBAC/connectivity); internal
  bugs get `reasons.Internal` and no hints.
- Hints are commands to run or paths to check, attached as `[]string`, never a
  bare string.
- Never decorate an operand of `errors.Join` — decoration goes on top of the join.

**Reason vocabulary**

- One shared vocabulary in `pkg/reasons` (a leaf package), not per-package reason
  files — reasons classify *what the operator must do*, not which package failed.
- Prefer an existing constant; declare a new one only in the PR that first uses
  it, and only when some consumer would treat it differently.
- Package-local reasons are allowed for genuinely subsystem-specific failure modes.
- Never rename or remove a shipped reason — the strings live in operator logs and
  scripts.

**Rendering**

- Human surfaces render through `doctor.OperatorMessage`, never `err.Error()` —
  the printable `{reason: X}` pair belongs in logs only.
- `errx.Format()` is not used — the doctor panel's numbered resolution block is
  richer.
- **The error is the only hint channel.** Hints reach the panel by being attached
  to the error; `CheckErr` takes no hints alongside it, and a failure report's
  `Metadata` carries no `"instructions"` key. See *One hint channel* below.

**Process**

- Incremental per-package PRs, not one big refactor.
- Each migration PR ships a boundary-decoration table test (one row per boundary:
  expected reason + a hint fragment) and pins that neither the message nor the
  `errorx` namespace changed.
- Each PR rewrites its own package's legacy bare-string hint sites — no upfront
  cross-cutting conversion pass.

---

## Usage

```go
// Reason only — nothing the operator can act on.
return errx.WithReason(
    errorx.InternalError.Wrap(err, "failed to stat file: %s", path),
    reasons.Internal,
)

// Reason plus operator-actionable hints — the common case.
return errx.Decorate(
    errorx.IllegalArgument.New("file does not exist: %s", path),
    reasons.FileMissing,
    "Check the path and that the file exists and is a regular file.",
)
```

`errx.ReasonOf(err)` and `errx.Hints(err)` read them back. Both follow the
`errorx` cause chain *and* `errors.Unwrap`; when several layers carry the same
property, **the outermost wins**. Neither sees inside `errors.Join` — never
decorate a join operand; see the warning under *Compatibility during the
migration*.

Attach hints only where a real remediation exists — config, permissions,
connectivity, a flag value. Internal bugs get a reason and no hints.

---

## Decorate at boundaries only

**Decorate where the error leaves the package** — an exported `error` return, an
`automa.StepFailureReport`, or a `RunE` return in `cmd/`. Everything else keeps a
plain `errorx.<Type>.Wrap` with nothing attached.

One operator-visible failure is often four or five stacked wraps. Decorating each
duplicates hints in the output and, since the outermost reason wins, silently
overwrites the reason as the error propagates.

---

## Typed errors vs reason codes

**Default: do not declare a namespace or a typed error. Use the built-in
`errorx.<Type>` and let the reason code carry the classification.** That is what
issue #933's "package-level error types" requirement was for, and a reason code
does the job better — it survives a type rename and lines up with the log's
`reason=` field.

A local namespace is not free. `doctor.toErrorCode` keys off `errorx.IsOfType`
against the built-ins, so the same error declared three ways reports two codes:

| Declaration | `IsOfType(…, IllegalArgument)` | Reported code |
|---|---|---|
| `errorx.IllegalArgument.New(…)` | true | 10400 |
| `errorx.IllegalArgument.NewSubtype("invalid_identifier")` | true | 10400 |
| `errorx.NewNamespace("sanity").NewType("invalid_identifier")` | **false** | **10500** |

Traits do survive a local namespace — `NewNamespace("x", errorx.NotFound())` still
yields 10404 — but parent-type identity does not.
`TestToErrorCode_NamespaceChoice` pins all of it.

So:

1. **Default — no new type.** Built-in namespace + `errx.Decorate`, as in
   `pkg/sanity`.
2. **A caller must branch on this exact error** — declare it in that package's
   `errors.go` as `errorx.<Builtin>.NewSubtype("…")`. You get a distinct type to
   match while keeping `IsOfType` and the reported code. This is the only reason
   the repo has typed errors today (`state.NotFoundError`, `helm.ErrNotFound`,
   `principal.UserNotFoundError`, `daemon.ErrConfigNotFound`, `fsx.FileNotFound`,
   `os.ErrSwapDeviceNotFound`); nothing branches on a built-in except `doctor`.
3. **Already-namespaced packages stay as they are.** Adding reasons and hints is
   orthogonal to renaming types, and a rename churns `IsOfType` call sites and
   reported codes for no gain.

`NewSubtype` inherits its parent's namespace, so the type reads
`common.illegal_argument.invalid_identifier` — the name does not identify the
package. If package-identifying type names ever matter more than the code, move
`toErrorCode` onto `reasons.*` first, in its own PR; after that type identity
stops mattering. Do not fold that into a package migration — the codes are
operator-observable.

### Deviations from issue #933

Recorded here so ten migration PRs do not each re-derive them:

- *"Package-level error types"* → reason codes, per the rule above.
- *"Leverage `errx.Format()` for display"* → `doctor.OperatorMessage` plus
  doctor's own numbered resolution block, which is richer than `Format`'s flat
  output. `errx.Format` is unused.
- *`errorx.Temporary()`* → **not adopted.** Nothing in solo-weaver retries, and
  nothing reads the trait.

---

## Reason codes

The shared vocabulary is `pkg/reasons` — PascalCase, so an error, a log line's
`reason=` field and any `/status` output share one identifier. It is a **leaf
package** (imports only `errx`); it cannot live in `pkg/models`, which already
depends on `pkg/sanity`.

Reasons classify *what the operator must do*, not which package failed. **Prefer
an existing constant** — a new one is earned only when some consumer (`doctor`, a
log filter, an exit code) would treat it differently.

### Target vocabulary

The ~100 pre-errx hint sites were audited for #933 and their failure modes cluster
into the rows below. Every migration PR picks its reason from here instead of
inventing one. A constant is **declared in `pkg/reasons` only in the PR that first
uses it** — reserved rows live in this table until their package migrates. If a
failure mode genuinely fits no row, add a row in the same PR that declares the
constant.

| Reason | Meaning (operator's view) | Status |
|---|---|---|
| `InvalidArgument` | A flag value, flag combination or positional argument was rejected | **declared** — `pkg/sanity` |
| `FileMissing` | A required file, directory, binary or unit file does not exist and will not be created | **declared** — `pkg/sanity` |
| `Internal` | An invariant breach or bug in solo-weaver, not an operator error | **declared** — `pkg/sanity` |
| `PermissionDenied` | Superuser privilege required, or file/resource ownership or access denied | **declared** — `pkg/sanity` |
| `NotInstalled` | A component is absent; the operator must run its install command first | **declared** — `internal/workflows`; reserved for `internal/bll` handlers, `internal/network` ("run `… create` first") |
| `Unreachable` | An endpoint did not respond: K8s API server, daemon socket, block-node health | reserved — `internal/kube`, daemon steps, `internal/blocknode` |
| `K8sAPIFailed` | A Kubernetes API operation failed (ConfigMaps, ServiceAccounts, …) | reserved — `internal/kube`, cluster steps |
| `HelmOperationFailed` | A Helm install/upgrade/uninstall/render failed | reserved — `pkg/helm`, workflow steps |
| `ServiceOperationFailed` | A systemd unit operation failed, or a unit is in the wrong state | reserved — `pkg/os`, daemon/service steps |
| `DownloadFailed` | An artifact download failed: network error, HTTP status, truncated body — retrying is reasonable | reserved — `pkg/software` |
| `IntegrityCheckFailed` | A checksum, signature or tamper check failed on a downloaded or installed artifact — do **not** retry; report it | reserved — `pkg/software`, `pkg/helm` |
| `PreflightFailed` | A hardware/OS validation floor was not met (memory, storage, OS version) | reserved — `internal/workflows` preflight, `pkg/hardware` |
| `ResourceNotReady` | A Kubernetes resource did not converge in time (pod/CRD/PVC); the API call itself succeeded | reserved — `internal/kube`, `internal/blocknode`, workflow steps |
| `StateOperationFailed` | Reading, writing, refreshing or migrating the application state file failed | reserved — `internal/state`, `internal/rsl`, `internal/bll` |
| `ConfigInvalid` | A configuration **file's contents** are malformed or incomplete (`config.yaml`, `daemon.yaml`) — not a flag value | reserved — `pkg/config`, `cmd/daemon` |
| `PreconditionNotMet` | The system is in the wrong state for this command (already installed, service already running, resource still referenced); change the state or use a different verb | **declared** — `internal/workflows`; reserved for `internal/bll`, daemon/service steps |

Two mapping calls settled during the #933 audit, recorded so they stay uniform:
a requested version/platform absent from the embedded catalog is
`InvalidArgument` (the operator picked a value we don't ship), not
`NotInstalled`; and "already installed / already running" is
`PreconditionNotMet` — there is deliberately no `AlreadyInstalled` twin.

**Never rename or remove a shipped reason.** The strings appear in operator logs
and possibly in scripts that grep for them, so a rename silently reclassifies
every existing call site. Add the better name and leave the old constant in place.

For a genuinely subsystem-specific failure mode, declare a package-local reason
rather than widening the shared set — they work everywhere a shared one does:

```go
// internal/blocknode/reasons.go
const ReasonStorageLayoutMismatch errx.Reason = "BlockNodeStorageLayoutMismatch"
```

---

## How a reason reaches the operator

`Diagnose` surfaces the reason as `ErrorDiagnosis.Reason` (printed in the `-V`
panel); `CheckErr` writes it as a `reason=` field on the failure log line, with
any attached hints alongside as a `hints=` array — the log file alone carries
the remediation steps. Hints on the panel are resolved by `findResolution`,
which returns the attached hints or falls back to the `errorx`-namespace text.

`errx.PropertyReason` is registered **printable**, so `err.Error()` renders
`… bad flag value {reason: InvalidArgument}`. That is wanted in a log line and is
noise on a human surface, hence the split:

| Surface | Renders | Reason inline? |
|---|---|---|
| log line, `%+v` dump | `err.Error()` | yes |
| TUI step/phase failure line | `doctor.OperatorMessage(err)` | no |
| panel `Cause:` line | `doctor.OperatorMessage(err)` | no |
| panel `Reason:` line (`-V`) | `ErrorDiagnosis.Reason` | it *is* the reason |

`OperatorMessage` is `err.Error()` minus every reason pair along the cause chain;
the message, the `errorx` type, other printable properties, the `(hidden: …)`
underlying error and the cause chain are untouched. **Render errors on a new human
surface through it, not through `err.Error()`.** The removal is textual because
`errorx` cannot render a message minus one printable property; the real fix is
upstream in `errx`, after which `OperatorMessage` collapses to `err.Error()`.

It walks the chain the same way `errx` does — `errorx` causes then
`errors.Unwrap` — so it strips a nested reason left by stacked decoration, but not
one attached to a `WithUnderlyingErrors` operand, which `errx.ReasonOf` cannot see
either. One known gap remains: the default (non-`-V`) panel strips the reason
without printing it anywhere.

There is **no reason-to-exit-code mapping** — `CheckErr` exits 1 for every
failure. Adding one is a separate change, since operator scripts observe the
current value.

---

## One hint channel

Remediation reaches the operator by **one** route: hints attached to the error,
read back by `findResolution` into `ErrorDiagnosis.Resolution`, rendered as the
numbered `Resolution:` block by both the compact and the `-V` panel.

`CheckErr(ctx, err)` takes no hints beside the error, and nothing reads an
`"instructions"` key out of a report's `Metadata`. Two earlier channels did:

- a variadic `instructions ...string` blob on `CheckErr`, and
- `automa.WithMetadata(map[string]string{"instructions": …})` on a failure report.

Both were removed. They were not merely redundant: hints round-tripped
`[]string` → `strings.Join("\n")` → `strings.Split("\n")` on the way to the
panel, and the same hints rendered **numbered** through `Resolution` but
**unnumbered** through the blob, decided by nothing more than which channel a
given call site happened to populate. The blob also invited a printf mistake the
signature could not reject — `CheckErr(ctx, err, "failed to set flag %s", name)`
printed the format string verbatim and dropped `name`.

A step that wants the operator to see a command therefore decorates the error it
reports, and writes only genuine key/value facts into `Metadata`:

```go
return automa.FailureReport(stp,
    automa.WithError(errx.Decorate(
        errorx.IllegalState.New("weaver owner group %q has incorrect GID: expected %s, got %s",
            name, want, got),
        reasons.PreconditionNotMet,
        fmt.Sprintf("sudo groupmod -g %s %s", want, name),
        "Re-run this command once the GID matches.")),
    automa.WithMetadata(meta))
```

Note the split the removal forces, and which is worth keeping on its own:
**observed-vs-expected facts belong in the message, commands belong in the
hints.** The blobs interleaved the two, so every fact was stated twice.

`CheckReportErr` finds that error with `doctor.DeepestFailureError`, which
descends to the leaf failed step: automa replaces an enclosing step's error with
a fresh `workflow "X" completed with N step failures` wrapper that carries
neither reason nor hints, so diagnosing the top-level error would silently drop
both. `cmd/cli/commands/common` uses the same helper to pick the error it returns
from `RunE`, so the panel and the returned error agree.

---

## Testing decoration

A package that decorates its boundary errors carries a table test asserting it —
see `TestValidatorsAreDecorated` in `pkg/sanity/errors_test.go`: one row per
boundary, each asserting the expected reason and a distinctive fragment of the
hint. Decoration is a large, mechanical diff, and a table a reviewer can read in a
minute is the only practical way to check it without auditing every call site.
Also pin that decoration changed neither the message nor the `errorx` namespace.

---

## Compatibility during the migration

`models.ErrPropertyResolution` and `models.ErrPropertyReason` are **aliases** of
`errx.PropertyResolution` / `errx.PropertyReason`, not independent registrations.
`errorx` compares properties **by pointer, not by label**, so a second property
also labelled `"resolution"` would be a distinct key that `errx.Hints` cannot see,
silently dropping every pre-existing hint. `TestPropertyIdentity` pins it. Because
of the aliasing, the ~100 legacy `WithProperty(ErrPropertyResolution, …)` sites and
new `errx.WithHints` calls write to the same place.

**New code must attach a `[]string`, never a bare `string`** — `errx.Hints`
type-asserts to `[]string` and silently returns "no hints" for anything else. 14
legacy sites still pass a bare string (`internal/bll/blocknode`, `internal/rsl`,
`internal/workflows`, `cmd/cli`); each is rewritten to `errx.Decorate` by its own
package's migration PR rather than converted upfront, which would only pre-touch
files those PRs rewrite. Until then the coercion branch in `resolutionHints` is
load-bearing, and it stays afterwards because `ErrPropertyResolution` is exported.

Read every property through `doctor.extractProperty`, which follows the `errorx`
cause chain **and** `errors.Unwrap` — exactly what `errx.extract` does. A reader
that walks only the `errorx` chain stops at a plain `%w` wrapper and then
disagrees with `errx` about whether an error carries hints at all.
`TestExtractPropertyMatchesErrx` pins that parity — including the shared blind
spot below, so a traversal fix must land in `errx` first, or in both at once.

**Neither traversal sees inside `errors.Join`.** A joined error exposes its
children through `Unwrap() []error`, which `errors.Unwrap` does not follow, so a
reason or hints attached to a join *operand* are invisible to `errx.ReasonOf`,
`errx.Hints` and the doctor alike (upstream errx v1.0.0 limitation). Never
decorate an operand of `errors.Join`. Decorate on top of the join, using a
transparent `errorx.Decorate` as the property carrier so `errors.Is` still
reaches the sentinel:

```go
// wrong — the reason and hints vanish at the join
return errors.Join(ErrAborted, errx.Decorate(cause, reasons.X, "hint"))

// right — metadata outermost, sentinel still matchable with errors.Is
return errx.Decorate(
    errorx.Decorate(errors.Join(ErrAborted, cause), "context for the operator"),
    reasons.X, "hint",
)
```

(`errorx.<Type>.Wrap` is not a substitute for `errorx.Decorate` here — it wraps
opaquely, which severs `errors.Is` from the sentinel.) `TestJoinHidesDecoration`
pins both halves; when errx learns to traverse joins, its failure message says
what to update.

Parity is over **traversal**, not the value check: `resolutionHints` deliberately
coerces a bare string to a one-element slice, and reports "no hints" for an empty
`[]string` so the namespace fallback fires. `TestFindResolution` pins both.

---

## Quick reference

| Task | Call |
|---|---|
| Build a typed error | `errorx.<Type>.New(…)` / `.Wrap(err, …)` |
| Classify it at a boundary | `errx.WithReason(err, reasons.…)` |
| Add operator hints | `errx.WithHints(err, "step", …)` |
| Both at once | `errx.Decorate(err, reason, hints…)` |
| Read the reason / hints | `errx.ReasonOf(err)` / `errx.Hints(err)` |
| Render + exit (CLI) | `doctor.CheckErr(ctx, err)` |
| Render a message on a human surface | `doctor.OperatorMessage(err)` |

## See also

- `.claude/skills/errorx-conventions/SKILL.md` — which `errorx` namespace to pick
- `pkg/reasons` — the shared reason vocabulary
- `internal/doctor/diagnose.go` — how a failure becomes operator-facing output
