---
name: errorx-conventions
description: Use this skill BEFORE writing or modifying any Go error construction in this repo — e.g. "add error handling", "return an error from this function", any edit that introduces a new `return err` site, any urge to type `errors.New` or `fmt.Errorf` in production code under `cmd/`, `internal/`, `pkg/`. Also read it before attaching a reason code or remediation hint with `errx` (`errx.Decorate`, `errx.WithReason`, `errx.WithHints`), picking a `reasons.*` constant from `pkg/reasons`, or declaring a new typed error / `errorx.NewNamespace`. Production code must use `github.com/joomcode/errorx` and is enforced by `golangci-lint` (forbidigo). Captures the rule, the namespace mapping table, when a typed error is justified, the sentinel pattern using `errors.Join`, the boundary rule for errx decoration, and the test-file exemption. Read this BEFORE picking an errorx namespace — the wrong namespace silently degrades the doctor layer's error styling and exit-code mapping.
---

# errorx conventions for solo-weaver

`github.com/joomcode/errorx` is the standard error library for all production code under `cmd/`, `internal/`, and `pkg/`. The stdlib `errors.New` and `fmt.Errorf` are **forbidden** there — the `.golangci.yml` `forbidigo` linter rejects new occurrences via the `lint:errorx` task wired into `task lint` and `task lint:check`.

This skill captures *which* `errorx.<Type>` to pick for each shape of error, when a typed error is justified, and the migration traps that catch the unwary.

> **Companion library — `errx`.** This skill answers *what kind* of error it is. `github.com/automa-saga/errx` answers *why it failed* and *what the operator should do*, via a reason code and remediation hints. It stores its data as `errorx` properties, so everything here still applies unchanged.
>
> When the error **crosses a package boundary** — an exported `error` return, an `automa.StepFailureReport`, or a `RunE` return in `cmd/` — also decorate it:
>
> ```go
> return errx.Decorate(
>     errorx.IllegalArgument.New("file does not exist: %s", path),
>     reasons.FileMissing,
>     "Check the path and that the file exists and is a regular file.",
> )
> ```
>
> Errors that stay inside the package get **no** decoration — decorating every layer duplicates hints and overwrites the reason as it propagates. Pick the reason from the target-vocabulary table in `docs/dev/error-handling.md`, which has the full contract.

---

## The rule

| Where | Allowed | Forbidden |
|---|---|---|
| Production code (`cmd/`, `internal/`, `pkg/`, excluding `*_test.go`) | `errorx.<Type>.New(...)`, `errorx.<Type>.Wrap(err, ...)` | `errors.New(...)`, `fmt.Errorf(...)` |
| Test files (`*_test.go`) | Anything — `errors.New`, `fmt.Errorf`, `errorx.*`, custom types | (linter is exempted) |
| Generated code (`*.gen.go`, `*_generated.go`, `/generated/`) | Whatever the generator emits | (linter is exempted) |

The lint scope is `new-from-rev: origin/main`: only **lines you change relative to main** are checked. Pre-existing neighbouring `fmt.Errorf` lines are not your problem unless you touch them.

`errors.Is`, `errors.As`, `errors.Join`, and `errors.Unwrap` remain allowed and **are the right tools** for testing typed/sentinel errors — `errorx` errors implement the `Unwrap()` contract. The skill bans construction primitives, not inspection primitives.

---

## Namespace mapping table

Pick the namespace by the *shape of the failure*, not by the file the code lives in.

| Call site shape | `errorx` target |
|---|---|
| File I/O failure (`os.Open`, `os.Stat`, `os.ReadFile`) | `errorx.ExternalError.Wrap` |
| Shell / exec command failure (`exec.Command`, `automa_steps.RunBashScript`) | `errorx.ExternalError.Wrap` |
| Helm / kubectl / Kubernetes API call failure | `errorx.ExternalError.Wrap` |
| `crypto/rand`, `net.Dial`, HTTP request failure | `errorx.ExternalError.Wrap` |
| Template render / `text/template` `Execute` failure | `errorx.InternalError.Wrap` |
| Internal computation that should not have failed but did | `errorx.InternalError.Wrap` |
| `json.Unmarshal` / `yaml.Unmarshal` / malformed payload | `errorx.IllegalFormat.Wrap` or `.New` |
| Catalog / config / schema validation failure | `errorx.IllegalFormat.New` |
| Invariant breach ("should never happen", unreachable type-switch default) | `errorx.AssertionFailed.New` |
| User-supplied CLI flag value, config value, or flag combination rejected | `errorx.IllegalArgument.New` or `.Wrap` |
| User cancelled; permission/authorization refused; policy rejection | `errorx.RejectedOperation.New` or `.Wrap` |
| Inconsistent runtime state (missing cluster resource, unexpected enum, wrong phase) | `errorx.IllegalState.New` |
| Hardware / environment doesn't meet requirements | `errorx.IllegalState.New` |

Two tie-breakers:

- When the namespace is genuinely ambiguous, prefer the one that gives the operator the most actionable signal in `internal/doctor/`. `IllegalArgument` ("the operator can fix their input") beats `IllegalState` ("something is wrong with the cluster") when the failing operation is gated by a flag the user just passed.
- A permission failure reading a file is the ambiguous case in practice: `ExternalError` when it is an I/O call that failed (`os.Stat` → `EACCES`), `RejectedOperation` when *we* are refusing the operation (the superuser check in `root.go`).
- The I/O rows cover an I/O call's *expected* failure modes. A residual "should not happen" branch left after those are handled takes `InternalError` — see the stat handling in `pkg/sanity/sanity.go`, where `EACCES` is `ExternalError` with hints and an unexplained stat failure is `InternalError` with `reasons.Internal`.

When wrapping, the message describes *what we were trying to do*, not the underlying cause — that is already in the wrapped error. `errorx.ExternalError.Wrap(err, "failed to open %s", path)`, not `"failed to open %s: %w"`.

---

## Declaring a typed error

**Default: don't.** Use a built-in `errorx.<Type>` from the table and let the `errx` reason code carry the classification. A typed error is justified only when some caller must **branch on this exact error** (`errorx.IsOfType` / `errors.Is`) and behave differently. That is the only reason the existing typed errors exist — `state.NotFoundError`, `helm.ErrNotFound`, `principal.UserNotFoundError`, `daemon.ErrConfigNotFound`, `fsx.FileNotFound`, `os.ErrSwapDeviceNotFound`.

When it *is* justified, declare it in the package's `errors.go` as a **subtype of the built-in**, not under a fresh namespace:

```go
// Right — still IsOfType(err, errorx.IllegalArgument), so doctor's code stays 10400.
var ErrInvalidIdentifier = errorx.IllegalArgument.NewSubtype("invalid_identifier")

// Wrong — a fresh namespace severs the built-in match; doctor reports 10500 instead.
var errNS = errorx.NewNamespace("sanity")
```

`doctor.toErrorCode` keys off `IsOfType` against the built-ins, so a fresh namespace silently changes the reported error code. Traits survive a fresh namespace; parent-type identity does not. `TestToErrorCode_NamespaceChoice` pins this.

The ~13 packages with an existing `errorx.NewNamespace` (mostly in an `errors.go`) are **grandfathered** — use them (`errorx.IsOfType(err, helm.ErrNotFound)`), don't rename them, and don't copy the pattern. Full rationale in `docs/dev/error-handling.md`.

---

## Sentinel errors — `errors.Join`, not `fmt.Errorf`

Define a sentinel once at package scope as an `errorx` typed value:

```go
// internal/ui/prompt/prompt.go
var ErrAborted = errorx.RejectedOperation.New("aborted by user")
```

To attach a runtime cause, **use `errors.Join`** — it produces a multi-error whose `errors.Is` walks both sides, so callers can match either the sentinel or the cause:

```go
return fmt.Errorf("%w: %w", ErrAborted, cause)  // wrong, and forbidden by the lint
return errors.Join(ErrAborted, cause)           // right
```

For a pure sentinel return with no cause, return the sentinel value directly.

**errx caveat:** a reason/hints attached to a join *operand* are invisible to `errx.ReasonOf`/`errx.Hints` (joins expose `Unwrap() []error`, which the traversal doesn't follow). At a decorated boundary, decorate on top of the join — `errx.Decorate(errorx.Decorate(errors.Join(ErrAborted, cause), "…"), reason, hints…)` — see docs/dev/error-handling.md.

---

## Migration traps

1. **Don't drop the `fmt` import** when the file still uses `fmt.Sprintf` / `fmt.Fprintln` / `fmt.Printf`. A `goimports` pass will tell you, but it's easy to over-trim by hand.

2. **Keep the format-string verbs intact.** `New` and `.Wrap` accept printf verbs; just drop the `%w` — `.Wrap(err, "...")` handles the wrapping.

3. **`errors.Is` against an `errorx`-typed sentinel needs the same variable identity** — declared once at package scope, never re-created per call. `errorx` types implement `Is(target error) bool` matching on value identity, so `errors.Is(err, ErrFoo)` holds whether `err` is `ErrFoo` directly or `errors.Join(ErrFoo, cause)`.

4. **Custom inner-wrapping constructors** (e.g. `software.NewExtractionError(innerErr, ...)`): convert the inner argument to `errorx.<Type>.New(...)` and leave the constructor alone.

---

## Why tests are exempted

Test assertions check error *shape* or identity against a test-local sentinel, not namespace, and tests don't flow through `internal/doctor/` — so the namespace metadata is unused and rich `errorx` typing only obscures the assertion. Wanting an `errorx` type in a test is a signal the behaviour belongs in a non-test helper.

---

## When to *not* return an error

Independent of the construct rule: don't add error returns the caller can't act on.

- "I detected drift but I'm continuing" — log at `Warn`, return `nil`.
- "This optional feature isn't configured" — return a zero value and `nil`; let the caller's nil-check skip it.
- Programming-error states reachable only via a broken refactor — `panic` with a descriptive message rather than `errorx.AssertionFailed.New`. `chartSpec` in `internal/workflows/steps/catalog.go` is the existing example: the surrounding code can't recover, so panicking surfaces the bug at the development boundary.
