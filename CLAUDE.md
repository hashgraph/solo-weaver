# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**solo-weaver** (binary: `solo-provisioner`) is a Go-based CLI for automating Kubernetes deployment of Hedera network components (block nodes, alloy clusters). It helps node operators migrate from traditional deployment to containerized infrastructure.

## Workflow Reminders

> These rules apply every time code is changed in this repository.

0. **When starting a new PR**, automatically:
   1. Fetch and check out the latest `main`: `git fetch origin && git checkout main && git pull origin main`
   2. Create and switch to a new branch named `<issue_number>-<short-description>` where `<issue_number>` is **zero-padded to 5 digits** (e.g. issue 123 → `00123-fix-block-node-crash`): `git checkout -b <issue_number>-<short-description>`
   - Ask the user for the GitHub issue number and a short description (kebab-case) if they haven't provided them.

1. **After every code change**, run `task lint` to auto-format the code before considering the work done.
2. **After finishing implementation**, generate a review guide at `docs/claude/reviews/<issue_number>-<short-description>.md` (e.g. `docs/claude/reviews/00477-fix-metallb-annotation-on-upgrade.md`) that includes:
   - A short summary of the problem and solution
   - A table of changed files with a one-line description each
   - A code review checklist (one bullet per key invariant to verify)
   - Commands to run unit and integration tests for the changed code
   - Step-by-step manual UAT instructions (with expected output snippets)
3. **Every commit** must use a [Conventional Commits](https://www.conventionalcommits.org/) subject line (e.g. `feat(scope): ...`, `fix(scope): ...`, `refactor(scope): ...`) and be created with both `-s` (sign-off) and `-S` (GPG sign) flags:
   ```bash
   git commit -sS -m "type(scope): short description"
   # or for multiline messages:
   git commit -sS -F /tmp/commit-msg.txt
   ```
4. **Before creating a new commit**, check whether the change logically belongs to an existing unpushed commit on the branch (`git log --oneline origin/<base>..HEAD`). If it does, amend that commit instead of adding a new one:
   ```bash
   git add <files>
   git commit --amend -sS --no-edit   # keep message, or drop --no-edit to reword
   # if the target is not HEAD, use interactive rebase:
   git rebase -i origin/<base>        # mark the target commit as 'edit', then amend
   ```

## Build & Development Commands

This project uses [Task](https://taskfile.dev) as the build system. All commands run via `task`.

```bash
# Build for current platform
task build:weaver GOOS=linux GOARCH=amd64

# Build all platforms
task build

# Run unit tests
task test:unit

# Run unit tests with verbose console output
task test:unit:verbose

# Run a single integration test
task test:integration:verbose TEST_NAME='^Test_StepKubeadm_Fresh_Integration$'

# Run with coverage for specific paths
task test:coverage TEST_PATHS=./pkg/software/... TEST_REGEX="."

# Lint (format check)
task lint:check

# Auto-format
task lint

# Check license headers
task license:check

# Regenerate mocks
task mocks

# Clean build artifacts
task clean
```

### Test Tags

- Unit tests: tagged `!integration` — run with `go test -race -cover -tags='!integration' ./internal/... ./pkg/... ./cmd/...`
- Integration tests: tagged `integration` — require a running environment
- Cluster-dependent tests: tagged `require_cluster` — require a Kubernetes cluster
- Integration test files follow the naming convention `*_it_test.go`

### Platform Constraints

Several packages (e.g. `internal/mount`) are Linux-only and will not compile on macOS. This means:

- **Unit tests must be run inside a UTM VM** to get full coverage. Use `task vm:test:unit`.
- **Integration tests must always be run inside a UTM VM.** Use:

```bash
# Run all integration tests in the UTM VM
task vm:test:integration

# Run a single integration test in the UTM VM
task vm:test:integration TEST_NAME='^Test_StepKubeadm_Fresh_Integration$'
```

- On macOS you can still build and test packages that have no Linux-only dependencies (e.g. `./internal/migration/...`, `./internal/state/...`, `./pkg/...`), but always validate the full suite in the VM before merging.
- The VM is a UTM-managed Linux guest. See `task vm:start`, `task vm:stop`, `task vm:status` for VM lifecycle management.

## Architecture

### CLI Layer (`cmd/weaver/`)

Entry point is `cmd/weaver/main.go`. Commands use [Cobra](https://github.com/spf13/cobra) organized hierarchically:

```
solo-provisioner
├── install              # Self-install the provisioner
├── uninstall            # Self-uninstall the provisioner
├── kube cluster         # Kubernetes cluster (install, uninstall)
├── block node           # Block node lifecycle (check, init, install, upgrade, reset, uninstall)
├── teleport             # Teleport integration
│   ├── node install     #   Install node-level teleport agent (SSH access)
│   ├── node uninstall   #   Uninstall node-level teleport agent
│   ├── cluster install  #   Install cluster-level teleport agent (kubectl access)
│   └── cluster uninstall#   Uninstall cluster-level teleport agent
├── alloy cluster        # Alloy observability cluster (install, uninstall)
├── version              # Print version information
└── (demo)               # TUI demo command (dev/debug use only)
```

Root persistent flags: `--config`, `--output`, `--version`/`-v`, `--log-level`, `--force`, `--skip-hardware-checks` (hidden), `--verbose`/`-V` (expanded step-by-step TUI output), `--non-interactive`.

Command-specific flags (on `block`, `kube`): `--profile`. Error control flags (on workflow commands like `block node`): `--stop-on-error`, `--rollback-on-error`, `--continue-on-error`.

### Business Logic (`internal/`)

Key packages:
- `internal/workflows/` — Multi-phase orchestration (preflight → setup → deploy → verify) using the [automa](https://github.com/automa-saga/automa) framework. Workflow steps live in `internal/workflows/steps/`. Notification helpers live in `internal/workflows/notify/`.
- `internal/migration/` — Scoped migration framework (startup migrations run before every CLI invocation; block-node migrations run during upgrades). See `docs/dev/migration-framework.md`.
- `internal/state/` — Application state management (cluster state, software state, atomic writes)
- `internal/blocknode/` — Block node provisioning logic and storage migrations
- `internal/kube/` — Kubernetes client and admin operations
- `internal/bll/` — Business logic layer with subpackages: `bll/blocknode/` (install, upgrade, reset, uninstall handlers), `bll/cluster/` (cluster install handler), `bll/teleport/` (node and cluster agent install/uninstall handlers)
- `internal/reality/` — Hardware detection/validation (machine, cluster, block node, teleport checkers)
- `internal/alloy/` — Grafana Alloy observability configuration and rendering
- `internal/doctor/` — Error handling, diagnostics, and styled CLI output
- `internal/mount/` — Linux-only mount operations (build-tagged `linux`)
- `internal/network/` — Network configuration
- `internal/nio/` — Network I/O utilities (stdout/stderr wrappers)
- `internal/paths/` — Path management utilities
- `internal/proxy/` — HTTP proxy activation from configuration (sets env vars for downstream tools)
- `internal/rsl/` — Runtime specification layer (machine, cluster, block node, teleport runtimes)
- `internal/sysctl/` — System control parameter management
- `internal/templates/` — Embedded template files (alloy, block-node, cilium, crio, health, kubeadm, metallb, sysctl, teleport)
- `internal/tomlx/` — TOML extension utilities
- `internal/ui/` — TUI rendering: verbose/non-interactive modes, console logging suppression, prompts (`ui/prompt/`), message models, and output capture (unix/windows)
- `internal/version/` — Version file management (VERSION, COMMIT)

### Public Packages (`pkg/`)

Reusable packages with defined interfaces and generated mocks:
- `pkg/fsx/` — Filesystem abstraction (interface + mock for testing)
- `pkg/software/` — Software management (interface + mock)
- `pkg/kernel/` — Kernel management (interface + mock)
- `pkg/security/principal/` — Security/principal management (interface + mock)
- `pkg/helm/` — Helm chart operations (interface + mock)
- `pkg/config/` — Configuration models (parsed via Viper)
- `pkg/models/` — Shared data models (profiles, paths, inputs, execution modes)

Additional packages:
- `pkg/collections/` — Data structure utilities (pairs)
- `pkg/deps/` — Dependency management
- `pkg/exit/` — Exit code handling
- `pkg/hardware/` — Hardware specs, validation, and requirements
- `pkg/os/` — OS operations (signals, swap, systemd)
- `pkg/sanity/` — Sanity/domain checks
- `pkg/semver/` — Semantic versioning utilities
- `pkg/version/` — Version printing

### Mocks

Mocks are generated with `mockgen` from interface files. When adding a new interface that needs mocking, add the `mockgen` invocation to the `mocks` task in `Taskfile.yaml`.

### Logging

Uses `github.com/automa-saga/logx` (zerolog-based). Trace IDs are initialized in `main.go` and propagated via context.

### Developer Documentation

Detailed framework docs live in `docs/dev/`:
- `migration-framework.md` — Migration system design and usage
- `functionality-test-suite.md` — Testing approach and patterns
- `acceptance-tests.md` — Acceptance/UAT test patterns
- `golden-image.md` — VM golden image setup
- `hidden-flags.md` — Undocumented/hidden flags reference
- `effective-value-resolution.md` — Flag and config value resolution
- `label_profiles.md` — Deployment profile label conventions
- `proxy.md` — HTTP proxy configuration and activation
- `tui-output.md` — TUI output modes and formatting
- `tui-workflow-mapping.md` — Mapping of workflow steps to TUI messages

## Key Conventions

- All dependencies are vendored in `/vendor` — run `go mod vendor` after updating `go.mod`
- Builds produce binaries at `bin/solo-provisioner-{OS}-{ARCH}`
- Deployment profiles: `local`, `perfnet`, `testnet`, `previewnet`, `mainnet`
- PR titles must follow [Conventional Commits](https://www.conventionalcommits.org/)
- License headers (SPDX) are required on all source files — enforced by `task license:check`