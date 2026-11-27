# Feature Gates — UAT / Test Guide

How to verify the feature-gate system (epic #1275) end to end. Design and background:
[feature-gates.md](feature-gates.md). Alphas under test today: `ConsensusNodeOperatorManaged`
(the `consensus` group) and `KubeClusterUpgrade` (`kube cluster upgrade`); both are ready
(`Default: true`).

## 1. Automated tests

Run on macOS (no Linux-only deps) or in the VM (`task vm:test:unit`):

```bash
go test ./internal/featuregate/ ./cmd/cli/commands/common/ ./cmd/cli/commands/feature/ ./cmd/cli/commands/
```

| Test | Pins |
|---|---|
| `internal/featuregate` env tests | env opt-in, `=0` kill switch, unknown / renamed / non-boolean var errors |
| `TestExperimentalReadyIn` | readiness: Alpha `Default:true` ready, `Default:false` dormant |
| `TestRegistryRemovalLint`, `TestRegistryBaselineLint`, `TestNoOrphanFeatures` | registry hygiene |
| `common` gate tests | hide/label, fail-closed wiring, nested and stacked gates, `GateBehavior`, flag gating |
| `feature` tests | listing, filters, JSON to stdout, global checks skipped |
| `TestRootHelpSurface_Golden` | visible `--help` surface (regenerate with `-update-golden` only when intended) |

## 2. Manual UAT

Use the built binary (`task build:cli`) on the UTM VM.

### U1. Alpha commands are hidden

```bash
solo-provisioner --help | grep -E 'consensus|upgrade'      # no output
solo-provisioner kube cluster --help | grep upgrade         # no output
solo-provisioner --help | tail -3                           # footer shows 'Activation version:  1'
```

### U2. Alpha commands are refused without opt-in

```bash
sudo solo-provisioner kube cluster upgrade --plan
```

Expected: fails with `KubeClusterUpgrade is an experimental feature and not supported in
production`, hint to pass `--experimental` or set
`SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE=1`. Same for a real run (not `--help`, which skips
pre-run hooks) of `sudo solo-provisioner consensus node install` (env var
`SOLO_PROVISIONER_ENABLE_CONSENSUS_NODE_OPERATOR_MANAGED`).

### U3. `--experimental` opts in (sudo-safe)

```bash
sudo solo-provisioner kube cluster upgrade --plan --experimental
```

Expected: the gate passes and the plan runs. The flag is scoped to that command. On a VM with no
cluster the plan then reports `no kubeconfig was found` — that is the plan, not the gate.

### U4. Env var opts in; `sudo` drops it

```bash
SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE=1 solo-provisioner kube cluster upgrade --plan   # fails: must run as superuser (not a gate refusal)
SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE=1 sudo solo-provisioner kube cluster upgrade --plan   # refused: sudo strips env
SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE=1 sudo -E solo-provisioner kube cluster upgrade --plan # passes the gate
```

### U5. Env `=0` is a kill switch

```bash
sudo env SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE=0 solo-provisioner kube cluster upgrade --plan --experimental
```

Expected: refused with `... is disabled by SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE=0`,
hint `Unset ... to allow it.`

### U6. Bad opt-in env vars fail loudly

```bash
SOLO_PROVISIONER_ENABLE_NOPE=1 solo-provisioner feature-gate list                 # unknown var, hint to run feature-gate list
SOLO_PROVISIONER_ENABLE_CLUSTER_UPGRADE=1 solo-provisioner feature-gate list       # hint: renamed to ..._KUBE_CLUSTER_UPGRADE
SOLO_PROVISIONER_ENABLE_KUBE_CLUSTER_UPGRADE=yes solo-provisioner feature-gate list # hint: set to 1/true or 0/false
```

### U7. `feature-gate list`

```bash
solo-provisioner feature-gate list                      # runs unprivileged, no install needed
solo-provisioner feature-gate list --maturity=alpha
solo-provisioner feature-gate list --maturity=ga        # 'No features match the given filters.'
solo-provisioner feature-gate list --enabled            # empty today (alphas are off by default)
solo-provisioner feature-gate list --activation-version=1
solo-provisioner feature-gate list -o json | jq '.features[].feature'
solo-provisioner feature-gate list --maturity=bogus     # error, hint lists valid values
solo-provisioner feature-gate list -o yaml              # error, hint: text or json
```

Expected: both alphas listed as `ALPHA`, `ACTIVE FROM 1`, `ENABLED no`, with the exact env var
names and a `--experimental` note. JSON goes to stdout (pipes cleanly).

### U8. Opt-in shows as enabled

```bash
SOLO_PROVISIONER_ENABLE_CONSENSUS_NODE_OPERATOR_MANAGED=1 solo-provisioner feature-gate list --enabled
```

Expected: `ConsensusNodeOperatorManaged` listed with `ENABLED yes`.

### U9. Nested gates do not fail open

```bash
sudo solo-provisioner consensus node install               # refused without opt-in (group gate runs)
sudo solo-provisioner consensus node install --experimental   # passes the gate; then fails 'profile flag is required'
```

## 3. Regression checks

- GA commands unchanged: `sudo solo-provisioner block node check`, `solo-provisioner version` work with no opt-in.
- `feature-gate` and `version` run without root; other commands still require it.
- Dormant features (`Alpha, Default:false`, add temporarily in `registry.go`): neither
  `--experimental` nor the env var enables them; refusal says 'cannot be enabled yet'.
