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
# Use Debian (default)
task vm:start

# Use Ubuntu 22.04
task vm:start VM_OS_TYPE=ubuntu

# Or use convenience tasks
task vm:ubuntu:start
task vm:debian:start

# View all available targets
task vm:targets
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

#### 2. Create the Golden Images

If this is your first time setting up, follow these steps:

##### Debian Installation

1. Start a new VM in UTM:
    - Select **Virtualize** → **Linux**.
    - Boot from the Debian ISO:  
      [Debian 13.1.0 ARM64 Netinst](https://cdimage.debian.org/cdimage/archive/13.1.0/arm64/iso-cd/debian-13.1.0-arm64-netinst.iso).
      - If the link is outdated, find the latest at [Debian CD Image Archive](https://www.debian.org/CD/http-ftp/#stable).
   - Storage: 64 GiB (default).
   - Shared Directory: set to your `solo-weaver` project root.
   - Network: **Bridged (Advanced)** on your Wi-Fi interface (e.g., `en0`).
   - Name the VM: `solo-weaver-debian-golden`.

2. Install Linux in the VM:
- Start the golden image installation.
- Keep all default selections except:
  - Language: **English (US)**.
  - Location: your country (e.g., **Australia**).
  - Locale: **en_US.UTF-8**.
  - Keyboard: **American English**.
  - Hostname: keep default `debian`.
  - Domain: leave empty.
  - Root password: leave empty.
  - User:
      - Full name: `provisioner`
      - Username: `provisioner`
      - Password: `provisioner`
  - Clock: default selection.
  - Partitioning:
      - Guided – use entire disk
      - All files in one partition
      - Write changes to disk
  - Package mirror: choose a local Debian mirror (e.g., `mirror.aarnet.edu.au`).
  - Software selection: keep default
- Remove the ISO from UTM (**CD/DVD → Clear**) before reboot.
- Select **Continue** and boot into Debian.

✅ At this point, your **debian golden image** is ready.

##### Ubuntu Installation

1. Start a new VM in UTM:
    - Select **Virtualize** → **Linux**.
    - Boot from the Ubuntu ISO:  
      [Ubuntu 22.04.5 LTS ARM64](https://cdimage.ubuntu.com/releases/jammy/release/ubuntu-22.04.5-live-server-arm64.iso).
   - Storage: 64 GiB (default).
   - Shared Directory: set to your `solo-weaver` project root.
   - Network: **Bridged (Advanced)** on your Wi-Fi interface (e.g., `en0`).
   - Name the VM: `solo-weaver-ubuntu-golden`.

2. Install Linux in the VM:
- Start the golden image installation.
- Keep all default selections except:
  - Language: **English (US)**.
  - Type of installation: **Ubuntu Server (minimized)**.
  - User:
      - Full name: `provisioner`
      - Server name: `ubuntu`
      - Username: `provisioner`
      - Password: `provisioner`
- Remove the ISO from UTM (**CD/DVD → Clear**) before reboot.
- Reboot and log in with:
    - Username: `provisioner`
    - Password: `provisioner`
- Install QEMU Guest Agents:
    ```sh
    sudo apt update
    sudo apt install qemu-guest-agent -y
    ```

✅ At this point, your **ubuntu golden image** is ready.

---

#### 3. Day-to-Day Debugging

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

