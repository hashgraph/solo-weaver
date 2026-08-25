# Troubleshooting

## Common problems

### Permission denied

Almost every command needs root.

```bash
sudo solo-provisioner block node install --profile=local
```

Exceptions: `solo-provisioner version`, and `block node reconcile-shaper --check`.

### `profile flag is required`

Every `block node` command needs `--profile`.

```bash
sudo solo-provisioner block node check --profile=mainnet
```

`kube cluster install` is the exception — it takes no profile. See
[Deployment profiles](reference/deployment-profiles.md).

### `invalid base path`

The storage path must exist and be writable.

```bash
sudo mkdir -p /mnt/storage
sudo solo-provisioner block node install --profile=mainnet --base-path=/mnt/storage
```

To change a storage path on an existing install you also need `--purge-storage` — a local PV's
`hostPath.path` is immutable. See
[`reconfigure`](commands/block-node.md#reconfigure--change-settings-without-changing-the-version).

### Helm chart problems

Pin the version explicitly instead of resolving the latest:

```bash
sudo solo-provisioner block node install --profile=mainnet --chart-version=0.22.1
```

If the install times out, raise `--timeout`. The default is `5m0s`, and exceeding it rolls the
operation back.

### Alloy fails: cluster not reachable

Alloy deploys into an existing cluster; it never creates one. Install the cluster first:

```bash
sudo solo-provisioner kube cluster install
```

### Locked out of SSH after enabling the firewall

The host firewall is default-drop. An empty management allowlist admits nobody.

- `block node install --firewall-enabled` with an empty `--mgmt-cidrs` **skips** the firewall
  on purpose, exactly to avoid this.
- `network firewall create` with no `--mgmt-cidrs` does **not** skip it — it renders an empty
  allowlist. Always pass `--mgmt-cidrs`.

Recovery needs console access. Then:

```bash
sudo solo-provisioner network firewall add --name mgmt --cidr <your-cidr>
```

### Corrupt firewall config

Every apply keeps the previous generation. Restore it and re-apply:

```bash
sudo cp /etc/solo-provisioner/network-weaver-host-firewall.yaml.prev \
        /etc/solo-provisioner/network-weaver-host-firewall.yaml
sudo solo-provisioner network firewall reapply
```

Full detail: [Recovering a corrupt config](commands/network/firewall.md#recovering-a-corrupt-config).

### Traffic shaping is on but nothing is being shaped

Check in this order:

1. **Is the daemon running?**
   ```bash
   sudo solo-provisioner daemon service check
   ```
2. **Do the policies exist, and do their sets have members?**
   ```bash
   sudo solo-provisioner network policy show
   ```
   Sets start empty. The daemon fills them from the block node's statusz, so an empty set
   usually means statusz is unreachable — see `--statusz-base-url`.
3. **Is traffic actually landing in a class?**
   ```bash
   sudo solo-provisioner network shape watch --device egress --iface enp0s1
   ```
   A non-zero rate against a class means classification is working.
4. **Is ingress shaping attached to the pod?** Ingress lives on the pod's host-side veth and is
   attached per-pod by the daemon. Find the veth with `ip link`, then:
   ```bash
   sudo solo-provisioner network shape watch --device ingress --iface lxc1a2b3c
   ```

### Rollback did not seem to run

`--rollback-on-error` is not shown in the TUI. Confirm it another way:

```bash
helm list -A
```

or read the workflow report YAML whose path is printed as `report_path=…`.

## Getting more detail

### Expanded output

```bash
sudo solo-provisioner block node install --profile=local --verbose
```

### Debug logs

```bash
sudo solo-provisioner block node install --profile=local --log-level=debug
```

or in the config file:

```yaml
log:
  level: debug
  consoleLogging: true
```

### Raw logs, no TUI

Useful in CI, or when the TUI is hiding something:

```bash
sudo solo-provisioner block node install --profile=local --non-interactive
```

### Machine-readable output

```bash
sudo solo-provisioner block node install --profile=local -o json \
  | jq 'select(.type=="summary")'
```

## Help

```bash
solo-provisioner --help
solo-provisioner block --help
solo-provisioner block node --help
solo-provisioner block node install --help
```

Still stuck? Open an issue at
[hashgraph/solo-weaver](https://github.com/hashgraph/solo-weaver/issues), and include the
workflow report YAML named in the `report_path=…` line.
