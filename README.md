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

## Contributing

Contributions are welcome! Please open issues or submit pull requests.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.