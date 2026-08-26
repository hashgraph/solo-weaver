# Deployment Profiles

A profile tells the provisioner which network you are deploying against. It selects hardware
requirements, sizing defaults, and labels.

| Profile | Network | Use it for |
|---|---|---|
| `local` | none (local only) | Development, CI |
| `perfnet` | Performance-testing network | Load testing |
| `testnet` | Hedera Testnet | Integration testing |
| `previewnet` | Hedera Previewnet | Preview / staging |
| `mainnet` | Hedera Mainnet | Production |

## Where the flag applies

- **Needs `--profile`:** every `block node` command, and `alloy cluster install`
  (where it sets the `environment` label for the `ops` label profile).
- **Does not take `--profile`:** `kube cluster install`. Cluster install is
  workload-agnostic — it checks only what Kubernetes itself needs. Workload sizing is
  checked later, at `block node check` / `block node install`, once the profile and plugin
  preset are known.

> `kube cluster install` still accepts `--profile` and `--node-type` as hidden flags so old
> scripts do not break, but the values are **ignored** and a notice is printed. Remove them.

## Example

```bash
sudo solo-provisioner block node install --profile=mainnet
sudo solo-provisioner block node check   --profile=testnet
```
