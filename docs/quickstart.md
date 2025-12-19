# Quickstart Guide

Below is a quickstart guide to get you up and running with Solo Weaver.

## Prerequisites

- Unix operating system (Tested on: Debian 13.1.0, Ubuntu 22.04)
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

Solo Weaver accepts a configuration file. See the documentation or comments in sample [config.yaml](../test/config/config.yaml) for
all options.

## Setup Block Node

Solo Weaver deploys a Kubernetes cluster and deploys a Hedera Block Node on it using a Helm chart. It comes with
pre-configured profiles for local, mainnet, and testnet deployment mode.

```
$ weaver block node -h
Manage lifecycle of a Hedera Block Node

Usage:
  weaver block node [flags]
  weaver block node [command]

Available Commands:
  check       Runs safety checks to validate system readiness for Hedera Block node
  install     Install a Hedera Block Node
  upgrade     Upgrade a Hedera Block Node

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
$ weaver block node install -h
Run safety checks, setup a K8s cluster and install a Hedera Block Node

Usage:
  weaver block node install [flags]

Aliases:
  install, setup

Flags:
  -h, --help                    help for install
  -f, --values string           Values file
      --chart-repo string       Helm chart repository URL
      --chart-version string    Helm chart version to use
      --namespace string        Kubernetes namespace for block node
      --release-name string     Helm release name
      --base-path string        Base path for all storage (used when individual paths are not specified)
      --archive-path string     Path for archive storage
      --live-path string        Path for live storage
      --log-path string         Path for log storage
      --live-size string        Size for live storage PV/PVC (e.g., 5Gi, 10Gi)
      --archive-size string     Size for archive storage PV/PVC (e.g., 5Gi, 10Gi)
      --log-size string         Size for log storage PV/PVC (e.g., 5Gi, 10Gi)

Global Flags:
  -c, --config string    config file path
  -o, --output string    Output format (yaml|json) (default "yaml")
  -p, --profile string   Deployment profiles [local perfnet testnet mainnet]
  -v, --version          Show version 
```

To set up a block node, run (use appropriate profile and values file as required):

``` 
sudo weaver block node install --profile <local | mainnet | testnet> --values <custom-values-file>

# For example to deploy with a 'local' profile (local dev testing), run the below command:
# sudo weaver block node install --profile=local 

# To install with custom storage sizes:
# sudo weaver block node install --profile=local --live-size=10Gi --archive-size=20Gi --log-size=5Gi
```

This command will take a while (~5mins) to complete as it sets up the entire environment. Keep an eye on the console logs.

## Upgrade Block Node

To upgrade an existing Hedera Block Node deployment with new configuration:

```
$ weaver block node upgrade -h
Upgrade an existing Hedera Block Node deployment with new configuration

Usage:
  weaver block node upgrade [flags]

Flags:
  -h, --help                    help for upgrade
  -f, --values string           Values file
      --no-reuse-values         Don't reuse the last release's values (resets to chart defaults)
      --chart-repo string       Helm chart repository URL
      --chart-version string    Helm chart version to use
      --namespace string        Kubernetes namespace for block node
      --release-name string     Helm release name
      --base-path string        Base path for all storage (used when individual paths are not specified)
      --archive-path string     Path for archive storage
      --live-path string        Path for live storage
      --log-path string         Path for log storage
      --live-size string        Size for live storage PV/PVC (e.g., 5Gi, 10Gi)
      --archive-size string     Size for archive storage PV/PVC (e.g., 5Gi, 10Gi)
      --log-size string         Size for log storage PV/PVC (e.g., 5Gi, 10Gi)

Global Flags:
  -c, --config string    config file path
  -o, --output string    Output format (yaml|json) (default "yaml")
  -p, --profile string   Deployment profiles [local perfnet testnet mainnet]
  -v, --version          Show version 
```

To upgrade a block node, run:

```
sudo weaver block node upgrade --profile <local | mainnet | testnet> --values <custom-values-file>

# For example to upgrade with a 'local' profile and custom chart version:
# sudo weaver block node upgrade --profile=local --chart-version=1.2.3

# To upgrade without reusing previous release values (reset to chart defaults):
# sudo weaver block node upgrade --profile=local --values=custom-values.yaml --no-reuse-values
```

