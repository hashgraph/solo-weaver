# 00669 — Startup migration: re-apply disabled Cilium acceleration to existing clusters

## Problem

PR #674 changed the embedded Cilium template
(`internal/templates/files/cilium/cilium-config.yaml`) to
`loadBalancer.acceleration: "disabled"` (tc-eBPF) so Cilium no longer attaches a
native XDP program to the host's public NIC — native XDP attach/reconcile resets
the ixgbe (Intel X550) PHY and flaps the link, dropping the node's public IP
(#669).

The template change only helps **new** clusters. On an already-provisioned node
the Cilium setup steps are guarded and never re-apply it:

- `configureCilium` skips when the installer's persisted `IsConfigured` flag is set.
- `installCiliumCNI` skips when `cilium status` succeeds (cluster already running).

So existing clusters keep `bpf-lb-acceleration: best-effort` (native XDP) and stay
exposed to the flap until manually fixed.

## Solution

Add a startup-scoped `CLIVersionMigration` (`CiliumAccelerationMigration`,
`internal/workflows/migration_cilium_acceleration.go`) that runs on the first
provisioner invocation after upgrading across the version boundary and brings
existing clusters onto the new template:

1. Read `bpf-lb-acceleration` from the live `cilium-config` ConfigMap.
2. No-op if it can't be read (node has no cluster) or is already `disabled`
   (migration already ran) — this makes it safely idempotent/one-shot regardless
   of when the on-disk provisioner version advances past the boundary.
3. Otherwise re-render `cilium-config.yaml` (`software.ReconfigureCiliumConfig`)
   and apply it with `cilium upgrade --values <config> --wait`.

`Applies()` is inherited from `CLIVersionMigration` and gates purely on the CLI
version boundary (`minVersion = 0.19.1`, the release shipping #674 + this
migration); all live-state checks live in `Execute` with safe no-op fallbacks.

`software.ReconfigureCiliumConfig` is a new exported helper that re-renders the
config to its canonical sandbox path; the existing install path
(`createCiliumConfigFile`) and it now share a `renderCiliumConfig` helper.

## Changed files

| File | Change |
| --- | --- |
| `internal/workflows/migration_cilium_acceleration.go` | New `CiliumAccelerationMigration` (startup `CLIVersionMigration`). |
| `internal/workflows/migration_cilium_acceleration_test.go` | Tests for `Applies` (version boundary) and `Execute` (already-disabled / no-cluster / best-effort). |
| `pkg/software/cilium_installer.go` | Extract `renderCiliumConfig`; add exported `ReconfigureCiliumConfig` + `CiliumConfigPath`. |
| `cmd/cli/commands/root.go` | Register the migration under `migration.ScopeStartup`. |
| `docs/claude/reviews/00669-cilium-acceleration-migration.md` | This review guide. |

## Review checklist

- [ ] `minVersion` (`ciliumAccelerationMinVersion`) equals the release that ships
      #674 + this migration (semantic-release bump since v0.19.0 → **0.19.1**;
      only `fix:`/`ci:` commits present). If a `feat:` lands first, bump to the
      resulting minor.
- [ ] Migration is registered under `ScopeStartup` after `LegacyBinaryMigration`.
- [ ] `Execute` no-ops when the ConfigMap read errors or returns `disabled`
      (idempotent / one-shot; safe across repeated invocations in the window).
- [ ] `cilium upgrade` uses `--values <rendered config>` and `--wait`.
- [ ] No change to install-time behaviour (`createCiliumConfigFile` still renders
      via the shared helper and writes through the fileManager).

## Tests

```bash
go test ./internal/workflows/... -run CiliumAcceleration -count=1
go test ./pkg/software/... -run Cilium -count=1
```

## Manual UAT

On an existing block-node host currently at `best-effort` (e.g. the testnet `blk`
nodes), after upgrading the provisioner binary to the release carrying this change:

```bash
# Before: live config still best-effort, native XDP attached to the public NIC.
KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n kube-system \
  get cm cilium-config -o jsonpath='{.data.bpf-lb-acceleration}'; echo   # best-effort

# Run any provisioner command (the startup migration fires in PersistentPreRun):
sudo solo-provisioner version

# After: live config flipped to disabled, no XDP on the public NIC, no carrier flap.
KUBECONFIG=/etc/kubernetes/admin.conf kubectl -n kube-system \
  get cm cilium-config -o jsonpath='{.data.bpf-lb-acceleration}'; echo   # disabled
for n in $(ls /sys/class/net | grep -E '^eno|^enp|^eth'); do
  echo -n "$n: "; ip -d link show "$n" | grep -qi xdp && echo "XDP ATTACHED (bad)" || echo "no xdp (good)"
done
journalctl -u systemd-networkd -b | grep -iE 'eno[0-9].*(Gained|Lost) carrier' | tail
```

Re-running a provisioner command must NOT re-trigger `cilium upgrade` (Execute
sees `disabled` and no-ops).
