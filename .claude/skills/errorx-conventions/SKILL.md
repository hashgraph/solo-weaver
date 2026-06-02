---
name: errorx-conventions
description: Use this skill BEFORE writing or modifying any Go error construction in this repo — e.g. "add error handling", "return an error from this function", any edit that introduces a new `return err` site, any urge to type `errors.New` or `fmt.Errorf` in production code under `cmd/`, `internal/`, `pkg/`. Production code must use `github.com/joomcode/errorx` and is enforced by `golangci-lint` (forbidigo). Captures the rule, the namespace mapping table, the sentinel pattern using `errors.Join`, and the test-file exemption. Read this BEFORE picking an errorx namespace — the wrong namespace silently degrades the doctor layer's error styling and exit-code mapping.
---

# errorx conventions for solo-weaver

`github.com/joomcode/errorx` is the standard error library for all production code under `cmd/`, `internal/`, and `pkg/`. The stdlib `errors.New` and `fmt.Errorf` are **forbidden** in production code — the `.golangci.yml` `forbidigo` linter rejects new occurrences via the `lint:errorx` task wired into `task lint` and `task lint:check`.

This skill captures *which* `errorx.<Type>` to pick for each shape of error and the few migration traps that catch the unwary.

---

## The rule

| Where | Allowed | Forbidden |
|---|---|---|
| Production code (`cmd/`, `internal/`, `pkg/`, excluding `*_test.go`) | `errorx.<Type>.New(...)`, `errorx.<Type>.Wrap(err, ...)` | `errors.New(...)`, `fmt.Errorf(...)` |
| Test files (`*_test.go`) | Anything — `errors.New`, `fmt.Errorf`, `errorx.*`, custom types | (linter is exempted) |
| Generated code (`*.gen.go`, `*_generated.go`, `/generated/`) | Whatever the generator emits | (linter is exempted) |

The lint scope is `new-from-rev: origin/main`: only **lines you change relative to main** are checked. Pre-existing neighbouring `fmt.Errorf` lines are not your problem unless you touch them.

`errors.Is`, `errors.As`, `errors.Join`, and `errors.Unwrap` remain allowed and **are the right tools** for testing typed/sentinel errors — `errorx` errors implement the `Unwrap()` contract and play well with the stdlib `errors` utility functions. The skill bans construction primitives, not inspection primitives.

---

## Namespace mapping table

Pick the namespace by the *shape of the failure*, not by the file the code lives in.

| Call site shape | `errorx` target | Example |
|---|---|---|
| File I/O failure (`os.Open`, `os.Stat`, `os.ReadFile`) | `errorx.ExternalError.Wrap(err, "...")` | `errorx.ExternalError.Wrap(err, "failed to open %s", path)` |
| Shell / exec command failure (`exec.Command`, `automa_steps.RunBashScript`) | `errorx.ExternalError.Wrap` | `errorx.ExternalError.Wrap(err, "failed to initialize cluster with kubeadm")` |
| Helm / kubectl / kubernetes API call failure | `errorx.ExternalError.Wrap` | `errorx.ExternalError.Wrap(err, "failed to read K8s Secret %q in namespace %q", name, ns)` |
| `crypto/rand`, `net.Dial`, HTTP request failure | `errorx.ExternalError.Wrap` | `errorx.ExternalError.Wrap(err, "endpoint %s is not reachable", url)` |
| Template render / `text/template` `Execute` failure | `errorx.InternalError.Wrap` | `errorx.InternalError.Wrap(err, "failed to render core config")` |
| Internal computation that should not have failed but did | `errorx.InternalError.Wrap` | `errorx.InternalError.Wrap(err, "failed to create kubeconfig manager")` |
| `json.Unmarshal` / `yaml.Unmarshal` / malformed payload | `errorx.IllegalFormat.Wrap` or `.New` | `errorx.IllegalFormat.New("invalid user entry at line %d, no colons present", index)` |
| Catalog / config / schema validation failure | `errorx.IllegalFormat.New` (or `.Wrap` over a sub-error) | `errorx.IllegalFormat.New("missing type (must be %q or %q)", classic, oci)` |
| Invariant breach ("should never happen", "redirect loop", unreachable type switch default) | `errorx.AssertionFailed.New` | `errorx.AssertionFailed.New("unsupported flag type: %T", zero)` |
| User-supplied CLI flag value, config value, or flag combination rejected | `errorx.IllegalArgument.New` or `.Wrap` | `errorx.IllegalArgument.New("--plugins is required when --plugin-preset=%s", custom)` |
| User cancelled an operation; permission/authorization refused; policy rejection | `errorx.RejectedOperation.New` or `.Wrap` | `errorx.RejectedOperation.New("aborted by user")` |
| Inconsistent runtime state (cluster missing required resource, unexpected enum value, wrong phase) | `errorx.IllegalState.New` | `errorx.IllegalState.New("K8s Secret %q not found in namespace %q", name, ns)` |
| Hardware / environment doesn't meet requirements | `errorx.IllegalState.New` | `errorx.IllegalState.New("CPU does not meet %s requirements (minimum %d cores)", t, n)` |

When the right namespace is genuinely ambiguous, prefer the one that gives the **user the most actionable signal** when surfaced by `internal/doctor/`. `IllegalArgument` ("the operator can fix their input") is preferable to `IllegalState` ("something is wrong with the cluster") when the failing operation is gated by a CLI flag the user just passed.

---

## Decision matrix — common solo-weaver shapes

These are the cases that come up most often when editing this codebase:

- `os.Stat(...) failed` / `os.Open(...) failed` → **`ExternalError.Wrap`**
- `kubectl get ... failed` / `kube.Client(...).Foo(...)` returned err → **`ExternalError.Wrap`**
- `helm install/upgrade/uninstall ...` returned err → **`ExternalError.Wrap`** (or one of the `pkg/helm/errors.go` typed errors if it's there — keep those)
- `automa_steps.RunBashScript(...)` returned err → **`ExternalError.Wrap`**
- Bad `--profile`, `--release-name`, `--plugins`, etc. → **`IllegalArgument.New`** (or `.Wrap` if there's an upstream parse error)
- Unexpected enum value, missing required state field after it should have been set → **`IllegalState.New`**
- "should never happen" type-switch default branch in a generic function → **`AssertionFailed.New`**
- User pressed Ctrl-C in a `huh` prompt; intentional cancel → **`RejectedOperation.New`**
- Template `Execute` failed; `tmpl.Parse` failed → **`InternalError.Wrap`**
- YAML/JSON unmarshal failed on data already in the binary or under our control → **`IllegalFormat.Wrap`**

When wrapping, the message should describe *what we were trying to do*, not the underlying cause (that's already in the wrapped error). `errorx.ExternalError.Wrap(err, "failed to open %s", path)` — not `"failed to open %s: %w"` and not `"open %s returned an error: %v"`. `errorx` handles the wrap formatting.

---

## Sentinel errors — `errors.Join`, not `fmt.Errorf`

When you need a sentinel that callers can detect with `errors.Is`, define it once at package scope as an `errorx` typed value:

```go
// internal/ui/prompt/prompt.go
var ErrAborted = errorx.RejectedOperation.New("aborted by user")
```

To wrap a sentinel with a runtime cause, **use `errors.Join`**, not `fmt.Errorf("%w: %w", ...)`. `errors.Join` produces a multi-error whose `errors.Is` walks both sides — callers can match either the sentinel or the cause:

```go
// Wrong (and forbidden by the lint):
return fmt.Errorf("%w: %w", ErrAborted, cause)

// Right:
return errors.Join(ErrAborted, cause)
```

`errors.Is(result, ErrAborted)` and `errors.Is(result, cause)` both return `true` after the `errors.Join` call. The `fmt.Errorf("%w: %w", ...)` pattern (Go 1.20+) is technically equivalent for inspection but uses the forbidden constructor, so the lint rejects it.

For pure sentinel returns (no runtime cause to attach), just return the sentinel value directly:

```go
if errors.Is(err, huh.ErrUserAborted) {
    return ErrAborted
}
```

---

## Migration traps

Things that catch people during a `fmt.Errorf` → `errorx.*.Wrap` conversion:

1. **Don't drop the `fmt` import when the file still uses `fmt.Sprintf` / `fmt.Fprintln` / `fmt.Printf`.** A `goimports` pass after the conversion will tell you, but it's easy to over-trim by hand. Look for any remaining `fmt.*` calls before deleting the import.

2. **Keep the format-string verbs intact.** `errorx.<Type>.New` and `.Wrap` accept printf-style verbs (`%s`, `%q`, `%d`, etc.). Just drop the `%w` for the wrapped error — `.Wrap(err, "...")` handles the wrapping for you. `errorx.ExternalError.Wrap(err, "failed to open %s", path)` — not `"failed to open %s: %w"`.

3. **`errors.Is` against an `errorx`-typed sentinel still works**, but the sentinel must be the same variable identity (`var ErrFoo = errorx.<Type>.New(...)` declared once at package scope, never re-created per call). `errorx` types implement an `Is(target error) bool` method that matches on value identity, so `errors.Is(err, ErrFoo)` returns `true` whether `err` is `ErrFoo` directly or `errors.Join(ErrFoo, cause)`.

4. **Custom inner-wrapping constructors** (e.g. `software.NewExtractionError(innerErr, ...)`): convert the inner argument to `errorx.<Type>.New(...)` rather than rewriting the constructor. The custom constructor stays; only the inner stdlib error becomes errorx.

5. **Existing custom namespaces in `pkg/os/errors.go`, `pkg/helm/errors.go`, `pkg/fsx/errors.go` are grandfathered** — they predate the standardisation and use `errorx.NewNamespace(...)` plus typed errors like `helm.ErrNotFound`. Don't replace them with the built-in namespaces; do use them via `errorx.IsOfType(err, helm.ErrNotFound)` when checking specific helm conditions.

6. **No new custom `errorx.NewNamespace` declarations.** Stick to the eight built-in `errorx.<Type>` namespaces in the mapping table. The custom namespaces above are technical debt we tolerate, not a pattern to copy.

---

## Why tests are exempted

`*_test.go` files keep `errors.New` / `fmt.Errorf` because:

- Test assertions usually check error *shape* (`require.ErrorContains(t, err, "...")`) or *identity* against a test-local sentinel, not namespace.
- Rich `errorx` typing in a test body adds noise that obscures what the assertion is actually checking.
- Tests are local to a package and don't flow through `internal/doctor/`, so the namespace metadata isn't used downstream.

If you find yourself wanting an `errorx` type in a test, it's a signal that the test is exercising production behaviour better expressed in a non-test helper.

---

## When to *not* return an error

Independent of the construct rule: don't add error returns that the caller can't act on. Some shapes that look like errors but aren't:

- "I detected drift but I'm continuing" — log at `Warn`, return `nil`.
- "This optional feature isn't configured" — return a zero value, `nil`, and let the caller's nil-check skip the feature.
- Genuine programming-error states reachable only via a broken refactor — `panic` with a descriptive message rather than `errorx.AssertionFailed.New`. `chartSpec` in `internal/workflows/steps/catalog.go` is the existing example: the surrounding code can't meaningfully recover, so panicking surfaces the bug at the development boundary.

Errors that *don't* belong here go via these three escape hatches, not via clever errorx typing.
