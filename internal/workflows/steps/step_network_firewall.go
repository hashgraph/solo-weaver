// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strconv"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/config"
)

// NetworkFirewallCreateStepId is the step ID for NetworkFirewallCreate, so
// callers building/inspecting a workflow (e.g. handler tests asserting the
// step list) can reference it instead of the literal string.
const NetworkFirewallCreateStepId = "network-firewall-create"

// newFirewallManager is the seam over the production firewall manager so unit
// tests can substitute a manager wired to a fake nft runner and temp paths
// (the production manager restarts a systemd service, which is Linux-only).
var newFirewallManager = func() *firewall.Manager { return firewall.NewManager() }

// NetworkFirewallCreate lays down the node-level `inet weaver-host-firewall` nftables table
// (SSH/management allowlist, ICMP policy, in-cluster host-service ports) by
// invoking the same logic as `network firewall create`. It is wired into the
// block-node workflow (`block node install` / `reconfigure` / `upgrade`) — not
// the generic `kube cluster install`, which provisions a cluster independent of
// any specific node type and should not unconditionally apply node-specific
// firewall rules.
//
// reconcile selects the convergence behaviour:
//   - reconcile=false (install / upgrade): create-if-missing. When the table
//     already exists the supplied flags are NOT applied, so re-running is a
//     no-op. This is the "re-assert the install decision, never regress" mode.
//   - reconcile=true (reconfigure): force re-render the table from the resolved
//     flags even when it already exists, so an operator changing firewall
//     settings via `block node reconfigure` actually sees them take effect. Only
//     the branch that already knows teardown is permitted passes this.
//
// The table's input chain is default-drop and the only SSH allow rule matches
// the management allowlist (`ip saddr @mgmt_addrs tcp dport <ssh> accept`).
// Applying it with an empty allowlist would drop every new SSH connection and
// lock the host out, so when no management CIDRs are configured this step SKIPS
// with a warning rather than rendering a lock-out ruleset. The allowlist is
// supplied via `--mgmt-cidrs` or the host.managementCidrs config value. An
// operator can also opt out entirely via `--firewall-enabled=false`.
func NetworkFirewallCreate(reconcile bool) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(NetworkFirewallCreateStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Applying host firewall (inet weaver-host-firewall)")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to apply host firewall")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Host firewall applied")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			hostCfg := config.Get().Host

			if hostCfg.Disabled {
				logx.As().Info().Msg("host firewall disabled (--firewall-enabled=false); skipping")
				return automa.SkippedReport(stp, automa.WithDetail("host firewall explicitly disabled by the operator"))
			}

			if len(hostCfg.ManagementCIDRs) == 0 {
				logx.As().Warn().Msg(
					"host firewall not applied: no management CIDRs configured. The inet weaver-host-firewall table uses a " +
						"default-drop input chain, so applying it without an SSH/management allowlist would lock " +
						"this host out of new SSH connections. Set host.managementCidrs in config or pass " +
						"`--mgmt-cidrs <cidr,...>` to 'block node install' (or 'reconfigure'/'upgrade' to retrofit " +
						"an already-provisioned host) to enable the host firewall. The rest of this command proceeds " +
						"unaffected — the host firewall is skipped, not the block node install.")
				return automa.SkippedReport(stp,
					automa.WithDetail("no management CIDRs configured; host firewall skipped to avoid SSH lock-out"))
			}

			mgr := newFirewallManager()

			// NewTable() seeds the design defaults (SSH 22, the stack in-cluster
			// port set). hostCfg is already the fully resolved effective config
			// (ResolveHostFirewallConfig applies flag > prompt > config file >
			// default precedence before this step ever runs), so every field is
			// applied unconditionally — including a deliberately empty PodCIDR
			// ("omit the rule") or InClusterPorts ("open no ports"). Only MgmtPorts
			// keeps an empty-slice guard, since an empty list would otherwise
			// indicate a config the resolver never touched.
			t := firewall.NewTable()
			t.Mgmt.CIDRs = hostCfg.ManagementCIDRs
			t.Blocked.CIDRs = hostCfg.BlockedCIDRs
			if len(hostCfg.MgmtPorts) > 0 {
				t.Mgmt.Ports = firewall.PortStrings(hostCfg.MgmtPorts)
			}
			t.InCluster.Ports = firewall.PortStrings(hostCfg.InClusterPorts)
			t.InCluster.CIDRs = nil
			if hostCfg.PodCIDR != "" {
				t.InCluster.CIDRs = []string{hostCfg.PodCIDR}
			}

			// Named allow rules are not part of this step's input: they are
			// declared with `network firewall create --from-file`, and config.yaml
			// has no field for them. Carry any that already exist across, or a
			// reconfigure (which force re-renders) would silently drop the
			// operator's k8s/Cilium/admin rules while appearing to succeed.
			if existing, err := mgr.Table(ctx); err == nil {
				t.Allow = existing.Allow
			}

			// Determine whether the table pre-existed so rollback only deletes a
			// table this step actually introduced. In create-if-missing mode
			// Create's returned `changed` already implies "did not exist", but in
			// reconcile (force) mode `changed` is true even for a re-render of a
			// pre-existing table — so probe first and never delete on rollback in
			// that case.
			existedBefore, err := mgr.IsActive(ctx)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			changed, err := mgr.Create(ctx, t, reconcile)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			createdByThisStep := changed && !existedBefore
			stp.State().Local().Set(FirewallCreatedByThisStep, createdByThisStep)

			meta := map[string]string{FirewallCreatedByThisStep: strconv.FormatBool(createdByThisStep)}
			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			// Only delete the table if this step created it; a pre-existing table
			// (create-if-missing found it already present) must be left intact.
			created, _ := stp.State().Local().Bool(FirewallCreatedByThisStep)
			if !created {
				return automa.SkippedReport(stp,
					automa.WithDetail("host firewall was not created by this step, skipping rollback"))
			}
			if err := newFirewallManager().Delete(ctx); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			return automa.SuccessReport(stp)
		})
}
