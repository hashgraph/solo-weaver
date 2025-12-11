# Quickstart Developer Guide

This guide provides instructions to set up the development environment for the **solo-weaver** project, including local
development, testing, and debugging using UTM VMs.

## Requirements

- Go 1.25 or newer
- Task (https://taskfile.dev/#/installation)
- Git
- UTM (for local development and debugging)
- rsync (for file synchronization with UTM VMs)
- (Optional) IntelliJ IDEA or GoLand for debugging

## Project Setup 

1. Clone the repository:

```sh
git clone https://github.com/hashgraph/solo-weaver.git
cd solo-weaver
task # to view available tasks
```
2. Project structure overview:
- `pkg/` – Core packages (e.g., `security/principal`, `fsx`)
- `cmd/` – CLI entry points
- `internal/` – Internal utilities

## Local Development, Testing & Debugging

The project supports debugging using UTM VMs for testing in a Linux environment similar to production.

### Debugging & Testing with UTM VM

The project supports debugging inside Linux VMs using [UTM](https://mac.getutm.app/) on macOS.  
This setup allows you to test **solo-weaver** in a Linux environment similar to production.

**Supported OS Types:**

- **Debian** (default)
- **Ubuntu 22.04**

You can work with either OS type or both simultaneously. See [VM Targets Documentation](VM_TARGETS.md) for detailed
usage.

**Quick Start with OS Type:**

```sh
# View all available targets
task vm:targets

# Use Debian (default)
# For ubuntu, specify VM_OS_TYPE=ubuntu
task vm:start
```

---

#### 1. Setup VM

Before creating the VM, make sure UTM and `rsync` are installed.  

This is handled automatically when you run:

```sh
task vm:start
```

**NOTE:** If no VM image exists yet, it will download **golden Debian image**, and you will need to restart UTM to load
the new image and set up shared folder (e.g. `solo-weaver`).
Remember you only need to set up the shared folder once. You will see instructions as below:

``` 
👉 Please restart UTM to reload VMs and set up the shared solo-weaver directory (`open -a UTM`).
```

Once you restart UTM and set the shared-folder `solo-weaver`, run the following command again to start the VM:

```sh
task vm:start
```

---

#### 2. Day-to-Day Debugging & Testing

Once you have started the vm, you can follow the instructions below to run and debug your application or tests inside
the VM directly from IntelliJ IDEA or other IDEs.

##### Run tests

1. Run unit tests inside the VM:
```
task vm:test:unit 
```

2. Run integration tests inside the VM (may take longer ~40mins):
```
task vm:test:integration
```

#### SSH into the VM to run the weaver CLI directly

1. SSH into the VM:
```sh
task vm:ssh
```

2. Inside the VM, first copy the weaver binary to `/tmp`:
```sh
# we need to copy the weaver binary to /tmp first rather than running from the mounted folder
cd /tmp && cp /mnt/solo-weaver/bin/weaver-linux-arm64 .

provisioner@debian:/tmp$ ./weaver-linux-arm64 -h
Solo Weaver - A user friendly tool to provision Hedera network components

Usage:
  weaver [flags]
  weaver [command]

Available Commands:
  install     Perform self-installation of Solo Weaver
  block       Manage a Hedera Block Node & its components
  version     Show version
  help        Help about any command
  completion  Generate the autocompletion script for the specified shell

Flags:
  -c, --config string   config file path
  -h, --help            help for weaver
  -o, --output string   Output format (yaml|json) (default "yaml")
  -v, --version         Show version

Use "weaver [command] --help" for more information about a command.
```

##### Debug using IntelliJ IDEA (Recommended)

The project includes pre-configured IntelliJ IDEA run configurations for seamless debugging:

- **`.idea/`** – IntelliJ project settings including remote debugging configurations
- **`.run/`** – Run configuration templates:
    - `Debug via Delve.run.xml` – Direct debugging configuration for VM debugging
    - `Template Go Remote.run.xml` – Remote debugging template
    - `Template Go Build.run.xml` – Build configuration template
    - `Template Go Test.run.xml` – Test debugging template

Simply use the **play buttons** in the IntelliJ debugger to start remote debugging sessions directly in the VM without
manual setup.

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

