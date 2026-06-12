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
existing clusters onto the new template. The provisioner can run before anything
is deployed, so it checks both install preconditions before touching Cilium:

1. **Kubernetes installed?** — the kubeadm admin kubeconfig
   (`/etc/kubernetes/admin.conf`) exists. If not, skip.
2. **Cilium installed?** — the `cilium-config` ConfigMap exists (read via the
   provisioner's own `kube.Client`). If the API is unreachable or the ConfigMap is
   absent, skip.
3. **Already done?** — if `bpf-lb-acceleration` is already `disabled`, skip. This
   makes the migration idempotent/one-shot regardless of when the on-disk
   provisioner version advances past the boundary.
4. Otherwise re-render `cilium-config.yaml` (`software.ReconfigureCiliumConfig`)
   and apply it with `cilium upgrade --values <config> --wait`.

The live-state reads use the provisioner's `internal/kube` client
(`kube.NewClient` + `ResourceExists` / `GetResourceNestedString`), not a `kubectl`
shell-out.

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
- [ ] `Execute` no-ops when Kubernetes is not installed (no admin.conf), when
      Cilium is not installed (no `cilium-config`), when the API is unreachable,
      or when acceleration is already `disabled` (idempotent / one-shot; safe
      across repeated invocations in the window and on undeployed hosts).
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
