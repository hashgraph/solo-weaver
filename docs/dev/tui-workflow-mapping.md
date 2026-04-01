# TUI–Workflow Mapping

How the Bubble Tea TUI display relates to automa workflow components.

---

## Overview

The [automa](https://github.com/automa-saga/automa) framework provides two
builder types: `WorkflowBuilder` (container of children) and `StepBuilder`
(leaf with execute logic). Automa has **no concept of "Phase"** — that is a
solo-weaver convention built on top of the `notify` callback layer.

The TUI depends on workflow authors calling the right `notify` callbacks. If a
`WorkflowBuilder` does not emit `PhaseStart`/`PhaseCompletion`, it is invisible
to the TUI — it serves only as an execution container.

### Coupling to automa types

The notify layer is **conceptually** a separate display concern, but it is
**currently coupled to automa types** through its callback signatures:

```go
// All Phase/Step callbacks require automa.Step and/or *automa.Report
PhaseStart      func(ctx context.Context, stp automa.Step, msg string, args ...interface{})
StepCompletion  func(ctx context.Context, stp automa.Step, report *automa.Report, msg string, args ...interface{})
```

The TUI handler uses `stp.Id()` to track which phase or step to update in its
model, and `report.Status` / `report.Duration()` / `report.Error` for rendering.
This means you **cannot** call `notify.As().PhaseStart(...)` from arbitrary
non-automa code without having an `automa.Step` reference.

**The one orthogonal path** is detail text via `logx`:

```go
logx.As().Info().Msgf("downloading %s", url)  // no automa types needed
```

This flows through the `logHook` → `StepDetailMsg` pipeline and appears as
transient grey text in the TUI without any automa dependency.

This coupling is a design choice, not a fundamental constraint — the notify
signatures could be refactored to accept plain string IDs if decoupling from
automa becomes necessary.

---

## Display layers

| TUI element | What the user sees | automa type | What makes it visible |
|---|---|---|---|
| **Phase** | Bold header, progress bar (level 0) or section with `•` header (level 1) | `WorkflowBuilder` with `WithPrepare` / `WithOnCompletion` / `WithOnFailure` | Lifecycle hooks call `notify.As().PhaseStart`, `PhaseCompletion`, `PhaseFailure` |
| **Step** | Single line with status icon (`✓`, `✗`, `⊘`, `⠋`) | `StepBuilder` with lifecycle hooks | Lifecycle hooks call `notify.As().StepStart`, `StepCompletion`, `StepFailure` |
| **Detail** | Transient grey text below the running step | No automa counterpart | `notify.As().StepDetail(...)` or `logx` messages forwarded by `logHook` |
| **Container workflow** | Nothing — invisible | `WorkflowBuilder` **without** Phase notify hooks | Not rendered; groups phases/steps for execution only |

### Key source files

- `internal/workflows/notify/handler.go` — `Handler` struct with all callback signatures
- `internal/ui/handler.go` — `NewTUIHandler()` converts notify callbacks to Bubble Tea messages
- `internal/ui/view.go` — renders phases and steps based on model state

---

## Indentation model

The TUI uses fixed indentation to communicate nesting. The indent level is
determined by whether a step is inside a named phase, not by automa's
`WorkflowBuilder` nesting depth.

### Level 0 — compact (default)

Each phase is a single line. Steps are hidden inside the progress bar:

```
  ✓ Preflight Checks  (1.2s)
  ✓ System Setup  (5.2s)
  ⠋ Kubernetes Setup  ████████████░░░░░░  67% (2m47s)  Setting up kubelet
```

| Element | Indent |
|---|---|
| Phase line (icon + name + bar/duration) | 2 spaces |

### Level 1 — verbose (`-V`)

Phases expand to show individual steps:

```
  • Preflight Checks
    ✓ Validating privileges  (0.2s)
    ✓ Validating service account  (0.3s)
    ⠋ Validating host profile
        detected: 8 cores, required: 4 cores
  ✓ Preflight Checks  (0.5s)

  • Kubernetes Setup
    ✓ Setting up kubelet  (2.3s)
    ⠋ Setting up kubectl
```

| Element | Indent |
|---|---|
| Phase header (`•`) | 2 spaces |
| Phase completion (`✓`/`✗`) | 2 spaces |
| Step inside a phase | 4 spaces |
| Detail text inside a phase | 8 spaces |

### Steps without a named phase

When steps are not wrapped in a phase (unnamed/default phase), they appear at
the top level:

```
  ✓ Installing solo-provisioner  (0.8s)
```

| Element | Indent |
|---|---|
| Step (no parent phase) | 2 spaces |
| Detail text (no parent phase) | 6 spaces |

The indentation logic lives in `internal/ui/handler.go` (`stepIndent()`) and
`internal/ui/view.go` (`indent` / `detailIndent` variables in
`renderPhaseExpanded`).

---

## How logs become detail text

When the TUI is active, `logx` messages are forwarded to the TUI as transient
detail text via a zerolog hook (`internal/ui/logging.go`):

```
logx.As().Info().Msgf("kubelet version: 1.33.4")
  │
  ├──▶ Log file (always written, all levels)
  │
  └──▶ logHook.Run()
        │
        ├── Level filter:
        │   Info, Warn       → always forwarded
        │   Debug            → forwarded only at -V (VerboseLevel ≥ 1)
        │   Error and above  → NOT forwarded (handled by notify.StepFailure)
        │   Trace and below  → never forwarded
        │
        ├── Throttle: skip if < 80ms since last send
        │
        ├── Sanitize: collapse whitespace, truncate at 200 chars
        │
        └──▶ program.Send(StepDetailMsg{Detail: "kubelet version: 1.33.4"})
              → appears as greyed text under the running step
```

**Implication for step authors:** the `Msg`/`Msgf` text of `Info`-level log
calls appears in the TUI. Make these messages human-friendly. Structured fields
(`Str`, `Int`, etc.) go only to the log file.

---

## Concrete example: block node install

The `NewBlockNodeInstallWorkflow` tree and how each node maps to a TUI element:

```
block-node-install                    ← Container workflow (invisible to TUI)
│
├── setup-kubernetes                  ← Container workflow (invisible)
│   │
│   ├── block-node-setup              ← Container workflow (invisible)
│   │   │
│   │   ├── Preflight Checks          ← Phase  (PhaseStart/PhaseCompletion)
│   │   │   ├── validate-privileges   ← Step   (StepStart/StepCompletion)
│   │   │   ├── validate-weaver-user  ← Step
│   │   │   ├── validate-host-profile ← Step
│   │   │   ├── validate-os           ← Step  (conditional, skipped with --skip-hardware-checks)
│   │   │   ├── validate-cpu          ← Step
│   │   │   ├── validate-memory       ← Step
│   │   │   └── validate-storage      ← Step
│   │   │
│   │   └── System Setup              ← Phase  (PhaseStart/PhaseCompletion)
│   │       ├── setup-directories     ← Step
│   │       ├── refresh-pkg-index     ← Step
│   │       ├── install-iptables      ← Step
│   │       └── ...                   ← (more package/kernel steps)
│   │
│   └── Kubernetes Setup              ← Phase  (PhaseStart/PhaseCompletion)
│       ├── disable-swap              ← Step
│       ├── configure-sysctl          ← Step
│       ├── setup-kubelet             ← Step
│       ├── setup-kubectl             ← Step
│       ├── setup-helm                ← Step
│       ├── setup-crio                ← Step
│       ├── setup-kubeadm             ← Step
│       ├── initialize-cluster        ← Step
│       ├── setup-cilium              ← Step
│       ├── start-cilium              ← Step
│       ├── setup-metallb             ← Step
│       ├── deploy-metrics-server     ← Step
│       └── check-cluster-health      ← Step
│
└── Block Node Deployment             ← Phase  (PhaseStart/PhaseCompletion)
    ├── setup-block-node-storage      ← Step
    ├── create-block-node-namespace   ← Step
    ├── create-block-node-pvs         ← Step
    ├── install-block-node            ← Step
    ├── annotate-block-node-service   ← Step
    └── wait-for-block-node           ← Step
```

At **level 0**, this renders as four progress-bar lines (one per phase). At
**level 1**, each phase expands to show its steps.

Source: `internal/workflows/blocknode.go`, `internal/workflows/cluster.go`,
`internal/workflows/setup.go`, `internal/workflows/preflight.go`,
`internal/workflows/steps/step_block_node.go`.

---

## Authoring guide

### Creating a Phase

A Phase is a `WorkflowBuilder` that emits `Phase*` notify callbacks. Use this
when you want a **visible group** in the TUI:

```go
func myPhase() *automa.WorkflowBuilder {
    return automa.NewWorkflowBuilder().
        WithId("my-phase").
        Steps(
            myStep1(),
            myStep2(),
        ).
        WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
            notify.As().PhaseStart(ctx, stp, "My Phase Name")
            return ctx, nil
        }).
        WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
            notify.As().PhaseFailure(ctx, stp, rpt, "My Phase Name")
        }).
        WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
            notify.As().PhaseCompletion(ctx, stp, rpt, "My Phase Name")
        })
}
```

### Creating a Step

A Step is a `StepBuilder` that emits `Step*` notify callbacks. Use this for
**individual operations**:

```go
func myStep() automa.Builder {
    return automa.NewStepBuilder().WithId("my-step").
        WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
            // do work
            return automa.SuccessReport(stp)
        }).
        WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
            notify.As().StepStart(ctx, stp, "Doing my thing")
            return ctx, nil
        }).
        WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
            notify.As().StepFailure(ctx, stp, rpt, "Doing my thing")
        }).
        WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
            notify.As().StepCompletion(ctx, stp, rpt, "Doing my thing")
        })
}
```

### Sending detail text

For transient status text visible under the running step:

```go
// Option 1: via notify (requires step reference)
notify.As().StepDetail(ctx, stp, "downloading %s", url)

// Option 2: via logx (preferred — also goes to log file)
logx.As().Info().Msgf("downloading %s", url)
```

### Creating a container workflow (invisible)

When you need to group phases without adding a visible TUI element:

```go
func myWorkflow() *automa.WorkflowBuilder {
    return automa.NewWorkflowBuilder().
        WithId("my-workflow").
        Steps(
            phaseA(),
            phaseB(),
        )
    // No WithPrepare/WithOnCompletion — invisible to TUI
}
```

### When to create a new phase

- The group of steps represents a **logical milestone** (e.g., "Preflight
  Checks", "Kubernetes Setup", "Block Node Deployment")
- The user would benefit from seeing **progress within** the group
- The phase name makes sense as a one-line summary at level 0

Avoid creating a phase for a single step — just use the step directly.
