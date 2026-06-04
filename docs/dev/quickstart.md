# Quickstart Developer Guide

This guide provides instructions to set up the development environment for the **solo-weaver** project, including local
development, testing, and debugging using UTM VMs.

## Requirements

- Go 1.25 or newer
- Task (https://taskfile.dev/#/installation)
- Git
- UTM (for local development and debugging)
- rsync (auto-installed via `task vm:install`; on macOS, Homebrew rsync is used because the system `/usr/bin/rsync` is Apple's openrsync and is incompatible with the VM's Samba rsync)
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

**NOTE:** If no VM image exists yet, it will download the **golden Debian image** and prompt you to restart UTM so it
picks up the new VM:

```
👉 Please restart UTM to reload VMs ('open -a UTM'), then run 'task vm:start'.
```

After restarting UTM, run `task vm:start` again to finish provisioning.

---

#### 2. Syncing host changes into the VM

The host source tree is **not** auto-mounted in the VM. Instead, the VM gets a one-way rsync copy at
`/mnt/solo-weaver`. Run `task vm:sync` after editing files on the host, after switching git worktrees, or any time you
want the VM to reflect the current host state:

```sh
task vm:sync
```

`task vm:test:unit`, `task vm:test:integration`, `task vm:alloy:start`, `task vm:teleport:start`, and the debug-server
tasks already declare `vm:sync` as a dependency, so the most common workflows stay one-command. Plain `task vm:ssh`
does **not** auto-sync — call `task vm:sync` first if you need fresh code inside the SSH session.

`task vm:sync` uses `rsync -az --delete`, so files removed on the host are also removed in the VM. The `bin/` and
`vendor/` directories are included (local builds happen on the host); `.git`, `.ssh`, and VM disk artifacts
(`*.iso`, `*.img`, `*.qcow2`) are excluded.

---

#### 3. Day-to-Day Debugging & Testing

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

#### SSH into the VM to run the Solo Provisioner CLI directly

1. SSH into the VM:
```sh
task vm:ssh
```

2. Inside the VM, first copy the solo-provisioner binary to `/tmp`:
```sh
# we need to copy the solo-provisioner binary to /tmp first rather than running from the mounted folder
cd /tmp && cp /mnt/solo-weaver/bin/solo-provisioner-linux-arm64 .

provisioner@debian:/tmp$ ./solo-provisioner-linux-arm64 -h
Solo Provisioner - A user friendly tool to provision Hedera network components

Usage:
  solo-provisioner [flags]
  solo-provisioner [command]

Available Commands:
  install     Perform self-installation of Solo Provisioner
  block       Manage a Hedera Block Node & its components
  version     Show version
  help        Help about any command
  completion  Generate the autocompletion script for the specified shell

Flags:
  -c, --config string   config file path
  -h, --help            help for solo-provisioner
  -o, --output string   Output format (yaml|json) (default "yaml")
  -v, --version         Show version

Use "solo-provisioner [command] --help" for more information about a command.
```

#### Using the Cache Proxy

A local cache proxy speeds up downloads during development by caching binaries, container images,
and Go modules. It can also be used to route traffic through corporate proxies for security or
compliance requirements.

1. Start the cache proxy on macOS:
```sh
task proxy:start
```

2. SSH into the VM **with proxy tunnels** (required — plain `task vm:ssh` does not set up tunnels):
```sh
task vm:ssh:proxy
```

3. Inside the VM, self-install and run with a proxy-enabled config:
```sh
cd /tmp && cp /mnt/solo-weaver/bin/solo-provisioner-linux-arm64 .
sudo ./solo-provisioner-linux-arm64 install
sudo solo-provisioner block node install -p local -c /mnt/solo-weaver/test/config/config_with_proxy.yaml
```

To verify traffic goes through the proxy, run `task proxy:status` on macOS to see Squid access logs
and cache hit/miss stats.

For full details, see [Proxy Support](proxy.md).

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

