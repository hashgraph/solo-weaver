# Hidden Flags

This document describes hidden CLI flags that are not shown in `--help` output. These flags exist for
support and debugging purposes and should not be used in normal operations.

## `--skip-hardware-checks`

**Scope:** `block node install`, `kube cluster install`

Skips OS, CPU, memory, and storage validation during installation workflows. The following preflight checks
still run even when this flag is set:

- Privilege validation (root user)
- Weaver user/group validation
- Host profile validation

**When to use:**

- Working around a false-positive hardware check failure.
- Development or testing on machines that don't meet production hardware requirements.

**Not supported by:** `block node check` — that command always runs all checks since its purpose
is to validate system requirements.

**Implementation:**

- Flag defined in `cmd/weaver/commands/common/flags.go` (`FlagSkipHardwareChecks`).
- Registered as a hidden persistent flag on the root command in `cmd/weaver/commands/root.go`.
- Read via `cmd.Flags().GetBool(common.FlagSkipHardwareChecks.Name)` in install commands.
- Passed as `skipHardwareChecks bool` through the workflow chain to `NewNodeSafetyCheckWorkflow`
  in `internal/workflows/preflight.go`, which conditionally excludes hardware steps.


