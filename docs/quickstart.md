# Quickstart Guide

## Features

- Automated resource provisioning and cleanup
- Simple YAML configuration
- Extensible and modular design
- User, group, and filesystem management utilities

## Requirements

- Go 1.25 or newer

## Installation

Install via Go:

```sh
go install github.com/hashgraph/solo-weaver@latest
```

Or clone the repository:

```sh
git clone https://github.com/hashgraph/solo-weaver.git
cd solo-weaver
task build
```

## Usage

Run with [Task](https://taskfile.dev/) (if used):

```sh
task run -- [flags]
```

Or run directly:

```sh
./weaver --config=config.yaml
```

## Configuration

Provide a `config.yaml` file with your desired settings.  
_See the documentation or comments in `test/config/config.example.yaml` for all options._

## Project Structure

- `pkg/` – Core packages (e.g., `security/principal`, `fsx`)
- `cmd/` – CLI entry points
- `internal/` – Internal utilities

## Debugging

The project supports debugging using UTM VMs for testing in a Linux environment similar to production.

### Debugging with UTM VM

The project supports debugging inside Linux VMs using [UTM](https://mac.getutm.app/) on macOS.  
This setup allows you to test **solo-weaver** in a Linux environment similar to production.

**Supported OS Types:**
- **Debian** (default)
- **Ubuntu 22.04**

You can work with either OS type or both simultaneously. See [VM Targets Documentation](VM_TARGETS.md) for detailed usage.

**Quick Start with OS Type:**
```sh
# View all available targets
task vm:targets

# Use Debian (default)
# For ubuntu, specify VM_OS_TYPE=ubuntu
task vm:start
```

---

#### 1. Start the VM Environment

Before creating the VM, make sure UTM and `rsync` are installed.  
This is handled automatically when you run:

```sh
task vm:start
```

If no VM image exists yet, you'll be prompted to create a **golden Debian image**.

---

#### 2. Day-to-Day Debugging

**Start the VM**
```sh
task vm:start
```

##### IntelliJ IDEA Integration (Recommended)

The project includes pre-configured IntelliJ IDEA run configurations for seamless debugging:

- **`.idea/`** – IntelliJ project settings including remote debugging configurations
- **`.run/`** – Run configuration templates:
  - `Debug via Delve.run.xml` – Direct debugging configuration for VM debugging
  - `Template Go Remote.run.xml` – Remote debugging template
  - `Template Go Build.run.xml` – Build configuration template  
  - `Template Go Test.run.xml` – Test debugging template

Simply use the **play buttons** in the IntelliJ debugger to start remote debugging sessions directly in the VM without manual setup.

##### Manual Delve Debugging (Alternative)

For other development environments or manual debugging setup:

1. **Run debugger against your app**
   ```sh
   task vm:debug:app -- block node setup
   ```

2. **Run debugger against tests**
   ```sh
   task vm:debug:test -- ./pkg/software
   ```

3. **Connect IDE Debugger**
    - Connect to `127.0.0.1:2345` with a **Go Remote** debug config.
    - Delve (`dlv`) is launched inside the VM and forwarded.
    - This approach is compatible with VS Code, GoLand, and other IDEs that support remote Go debugging.

#### Optional: SSH into the VM for manual operations

```sh
task vm:ssh
```

