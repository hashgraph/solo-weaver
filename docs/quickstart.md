# Quickstart Guide

Below is a quickstart guide to get you up and running with Solo Weaver.

## Prerequisites

- A Unix-like operating system (Linux)
- `curl` installed

## Install

- Run the single-line installer:

```
curl -sSL https://raw.githubusercontent.com/hashgraph/solo-weaver/main/install.sh | bash
```

- Verify installation:

```
weaver --help
```

## Configuration

Solo Weaver accepts a configuration file. _See the documentation or comments in `test/config/config.example.yaml` for
all
options._

## Setup Block Node

Solo Weaver deploys a Kubernetes cluster and deploys a Hedera Block Node on it using a Helm chart. It comes with
pre-configured profiles for local development, mainnet, and testnet.

```
$ ./weaver block node -h
Manage lifecycle of a Hedera Block Node

Usage:
  weaver block node [flags]
  weaver block node [command]

Available Commands:
  check       Runs safety checks to validate system readiness for Hedera Block node
  install     Install a Hedera Block Node

Flags:
  -h, --help   help for node

Global Flags:
  -c, --config string    config file path
  -o, --output string    Output format (yaml|json) (default "yaml")
  -p, --profile string   Deployment profiles [local perfnet testnet mainnet]
  -v, --version          Show version

Use "weaver block node [command] --help" for more information about a command. 
```

If you would like to customize the configuration, create a copy of the example config file and modify it as needed.
Also, if you would like to use custom BlockNode configurations, you can create a custom values file for BlockNode Helm
chart and pass using `--values` flag

```
$ ./weaver-linux-arm64 block node install -h
Run safety checks, setup a K8s cluster and install a Hedera Block Node

Usage:
  weaver block node install [flags]

Aliases:
  install, setup

Flags:
  -h, --help            help for install
  -f, --values string   Values file

Global Flags:
  -c, --config string    config file path
  -o, --output string    Output format (yaml|json) (default "yaml")
  -p, --profile string   Deployment profiles [local perfnet testnet mainnet]
  -v, --version          Show version 
```

To set up a block node, run (use appropriate profile and values file as required):

``` 
weaver block node install --profile <local | mainnet | testnet> --values <custom-values-file>
```

_This command will take a while to complete as it sets up the entire environment._
