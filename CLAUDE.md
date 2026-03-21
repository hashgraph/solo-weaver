# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**solo-weaver** (binary: `solo-provisioner`) is a Go-based CLI for automating Kubernetes deployment of Hedera network components (block nodes, alloy clusters). It helps node operators migrate from traditional deployment to containerized infrastructure.

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
├── block node     # Block node lifecycle (install, check, etc.)
├── alloy cluster  # Alloy cluster operations
├── kube           # Kubernetes operations
└── teleport       # Teleport integration
```

Global flags: `--config`, `--profile`, `--output`. Error control flags: `--stop-on-error`, `--rollback-on-error`, `--continue-on-error`.

### Business Logic (`internal/`)

Key packages:
- `internal/workflows/` — Multi-phase orchestration (preflight → setup → deploy → verify). Workflow steps live in `internal/workflows/steps/`.
- `internal/migration/` — Handles migration from legacy deployments
- `internal/state/` — Application state management
- `internal/blocknode/` — Block node provisioning logic
- `internal/kube/` — Kubernetes operations
- `internal/bll/` — Business logic layer
- `internal/reality/` — Hardware detection/validation

### Public Packages (`pkg/`)

Reusable packages with defined interfaces and generated mocks:
- `pkg/fsx/` — Filesystem abstraction (interface + mock for testing)
- `pkg/software/` — Software management (interface + mock)
- `pkg/kernel/` — Kernel management (interface + mock)
- `pkg/security/` — Security/principal management (interface + mock)
- `pkg/helm/` — Helm chart operations
- `pkg/config/` — Configuration models (parsed via Viper)
- `pkg/models/` — Shared data models

### Mocks

Mocks are generated with `mockgen` from interface files. When adding a new interface that needs mocking, add the `mockgen` invocation to the `mocks` task in `Taskfile.yaml`.

### Logging

Uses `github.com/automa-saga/logx` (zerolog-based). Trace IDs are initialized in `main.go` and propagated via context.

## Key Conventions

- All dependencies are vendored in `/vendor` — run `go mod vendor` after updating `go.mod`
- Builds produce binaries at `bin/solo-provisioner-{OS}-{ARCH}`
- Deployment profiles: `local`, `perfnet`, `testnet`, `previewnet`, `mainnet`
- PR titles must follow [Conventional Commits](https://www.conventionalcommits.org/)
- License headers (SPDX) are required on all source files — enforced by `task license:check`
