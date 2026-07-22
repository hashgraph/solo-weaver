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

// NetworkFirewallCreate lays down the node-level `inet host` nftables table
// (SSH/management allowlist, ICMP policy, in-cluster host-service ports) by
// invoking the same create-if-missing logic as `network firewall create`. It
// is wired into the block-node workflow (`block node install` /
// `reconfigure` / `upgrade`) — not the generic `kube cluster install`, which
// provisions a cluster independent of any specific node type and should not
// unconditionally apply node-specific firewall rules. The create is
// create-if-missing, so re-running any of those commands is a no-op.
//
// The table's input chain is default-drop and the only SSH allow rule matches
// the management allowlist (`ip saddr @mgmt_addrs tcp dport <ssh> accept`).
// Applying it with an empty allowlist would drop every new SSH connection and
// lock the host out, so when no management CIDRs are configured this step SKIPS
// with a warning rather than rendering a lock-out ruleset. The allowlist is
// supplied via `--mgmt-cidrs` or the host.managementCidrs config value. An
// operator can also opt out entirely via `--firewall-enabled=false`.
func NetworkFirewallCreate() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(NetworkFirewallCreateStepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Applying host firewall (inet host)")
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
					"host firewall not applied: no management CIDRs configured. The inet host table uses a " +
						"default-drop input chain, so applying it without an SSH/management allowlist would lock " +
						"this host out of new SSH connections. Set host.managementCidrs in config or pass " +
						"`--mgmt-cidrs <cidr,...>` to 'block node install' (or 'reconfigure'/'upgrade' to retrofit " +
						"an already-provisioned host) to enable the host firewall. The rest of this command proceeds " +
						"unaffected — the host firewall is skipped, not the block node install.")
				return automa.SkippedReport(stp,
					automa.WithDetail("no management CIDRs configured; host firewall skipped to avoid SSH lock-out"))
			}

			// NewTable() seeds the design defaults (SSH 22, the stack in-cluster
			// port set). hostCfg is already the fully resolved effective config
			// (ResolveHostFirewallConfig applies flag > prompt > config file >
			// default precedence before this step ever runs), so every field is
			// applied unconditionally — including a deliberately empty PodCIDR
			// ("omit the rule") or InClusterPorts ("open no ports"). Only SSHPort
			// keeps a zero-value guard, since 0 is never a valid port and would
			// otherwise indicate a config the resolver never touched.
			t := firewall.NewTable()
			t.MgmtCIDRs = hostCfg.ManagementCIDRs
			t.BlockedCIDRs = hostCfg.BlockedCIDRs
			if hostCfg.SSHPort != 0 {
				t.SSHPort = hostCfg.SSHPort
			}
			t.InClusterPorts = hostCfg.InClusterPorts
			t.PodCIDR = hostCfg.PodCIDR

			changed, err := newFirewallManager().Create(ctx, t, false)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			stp.State().Local().Set(FirewallCreatedByThisStep, changed)

			meta := map[string]string{FirewallCreatedByThisStep: strconv.FormatBool(changed)}
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
