# solo-provisioner

A Go-based tool for provisioning Hedera network components in a streamlined and automated way.

## Features

- Automated resource provisioning and cleanup
- Simple YAML configuration
- Extensible and modular design
- User, group, and filesystem management utilities

## Requirements

- Go 1.20 or newer

## Installation

Install via Go:

```sh
go install github.com/hashgraph/solo-provisioner@latest
```

Or clone the repository:

```sh
git clone https://github.com/hashgraph/solo-provisioner.git
cd solo-provisioner
task build
```

## Usage

Run with [Task](https://taskfile.dev/) (if used):

```sh
task run -- [flags]
```

Or run directly:

```sh
./solo-provisioner --config=config.yaml
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

### VM Debugging with UTM

For debugging in a native Linux environment using UTM virtual machines on macOS:

#### Golden Image Requirements

The VM debugging setup expects a UTM golden image with the following configuration:

- **ISO Source:** https://debian.mirror.digitalpacific.com.au/debian-cd/13.1.0/arm64/iso-cd/
- **Name:** `solo-provisioner-debian-golden`
- **Architecture:** arm64
- **Memory:** 4GB
- **Network:** Bridged
- **Bridge Interface:** en0
- **Shared Directory:** {project_root}
- **Default Username/Password:** provisioner/provisioner
- **Default Root Password:** `password`
- **Required packages:** sudo package installed

#### Debugging Steps

1. **Setup the UTM VM environment:**
   ```sh
   task vm:start    # Start the UTM VM
   ```

2. **Debug the application in VM:**
   ```sh
   task vm:debug:app [application-args]
   ```
   Example:
   ```sh
   task vm:debug:app -- block node setup
   ```

3. **Debug tests in VM:**
   ```sh
   task vm:debug:test [package]
   ```
   Example:
   ```sh
   task vm:debug:test -- ./pkg/software
   ```

4. **Connect your IDE debugger to 127.0.0.1 on port 2345** (debug uses port forward from VM to host). Use **Go Remote** debug configuration in your IDE.

**Optional: SSH into the VM for manual operations:**
```sh
task vm:ssh      # Connect to the VM via SSH
```

**Use VM Debugging with UTM for:**
- Testing system-level integrations
- Debugging Linux-specific functionality
- Validating production-like behavior
- Testing filesystem operations, systemd integration, or hardware detection
- End-to-end testing scenarios

**Note:** The debugging setup uses Delve debugger on port 2345 with API version 2.

## Contributing

Contributions are welcome! Please open issues or submit pull requests.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.