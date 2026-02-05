# Local Development

To develop locally, you need to use the provided Docker setup as below:

- Launch the container with `./launch-local.sh`
- In a separate terminal
    - Exec into the container: `docker exec -it solo-weaver-local`
    - Build: `cd /app && task build`
    - Run: `task run -- local node setup -c test/config/config.yaml`
    - Test system packages: `task test:system-packages`

Alternatively, to run system packages tests in one command (handles Docker setup automatically):
- `task test:system-packages:docker`

## Debugging

### Quick Start
1. **Setup debug container**: `./debug-setup.sh`
2. **Debug tests**: `./debug-run.sh test [package]`
3. **Debug app**: `./debug-run.sh app [args...]`

### IDE Setup (One-time)

**IntelliJ IDEA/GoLand:**
1. Run → Edit Configurations...
2. Click '+' → Go Remote
3. Create configuration:
   - Name: `Debug in Docker`, Host: localhost, Port: 2345

**VS Code:**
- Ensure Go extension is installed
- Use the provided launch configuration:
  - `🐳 Attach to Docker Container (Debug)` → localhost:2345

### Debugging Workflow

> **Note:** Both tests and application debugging use port 2345. Only run one debug session at a time.

**For Tests:**
1. Set breakpoints in test files (e.g., `pkg/software/system_packages_test.go`)
2. Run `./debug-run.sh test` (all tests) or `./debug-run.sh test ./pkg/semver` (specific package) - shows "API server listening at: [::]:2345"
3. Attach debugger (F5 in VS Code or Run → Debug in IntelliJ)
4. Tests run with breakpoints active

**For Application:**
1. Set breakpoints in source code (e.g., `cmd/weaver/main.go`)
2. Run `./debug-run.sh app cluster deploy` (waits for debugger)
3. Attach debugger (F5 in VS Code or Run → Debug in IntelliJ)  
4. Press Continue/Resume to start execution

### Available Commands

```bash
# Setup
./debug-setup.sh           # Setup debug container

# Debug scenarios  
./debug-run.sh test         # Debug all tests (default behavior)
./debug-run.sh test ./pkg/semver  # Debug specific package tests
./debug-run.sh app [args...]      # Debug solo-provisioner application

# Task aliases (from project root)
task debug                           # Setup debug environment and show help
task debug:container:setup              # Same as ./debug-setup.sh
task debug:container:test -- [package]  # Same as ./debug-run.sh test [package] (default: all tests)
task debug:container:app -- [args...]   # Same as ./debug-run.sh app [args...]

# Cleanup
docker stop solo-weaver-debug  # Stop debug container
```