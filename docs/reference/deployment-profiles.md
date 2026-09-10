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
- **Optional `--profile` (requires `--node-type`):** `kube cluster install`. By default it is
  workload-agnostic — checking only what Kubernetes itself needs — and workload sizing happens
  later at `block node check` / `block node install`. `--node-type` names the single workload
  the host is intended for; it may stand alone (validate only). Passing `--profile` opts into a
  workload-sized preflight floor and requires `--node-type` to select which floor to check.

> `kube cluster install` takes a single `--node-type` (the workload the host is intended for;
> may stand alone) and an optional `--profile` that requires `--node-type` to size the preflight
> hardware floor. Omit both for the substrate-only floor. Cluster install installs no
> operator/CRDs — the solo-operator is installed separately by `kube operator install`.

## Example

```bash
sudo solo-provisioner block node install --profile=mainnet
sudo solo-provisioner block node check   --profile=testnet
```
