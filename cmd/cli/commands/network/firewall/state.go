// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/joomcode/errorx"
)

// stateManager is the seam over the production state manager so command tests
// can substitute one backed by a temp state file.
var stateManager = func() (state.Manager, error) { return state.NewStateManager() }

// recordHostFirewallDecision records the operator's enable/disable decision into
// MachineState.Firewall so the standalone `network firewall` verbs and the
// block-node workflow share one source of truth.
//
// Without it the two paths disagree: `block node reconfigure` seeds its
// enable/disable choice from the persisted decision, which the standalone verbs
// never wrote, so a firewall created here read as "never configured" — and a
// later no-flag reconfigure resolved that to disabled and deleted it (issue
// #1003). The inverse matters just as much: after `delete --all`, a machine
// state still saying "enabled" would make the next reconfigure re-create the
// firewall the operator just removed.
//
// Only the decision is written. The CIDR/port content stays in the firewall's
// own config file, which ResolveHostFirewallConfig reads directly as a tier
// above machine state — mirroring it here would have to squeeze a Rule's port
// specs into HostConfig's []int and would silently drop any inclusive range.
//
// Best-effort by design: `network firewall create` is node-agnostic and may run
// on a host with no state file at all, and an nft ruleset that has already been
// applied to the kernel must not be reported as a failure because a bookkeeping
// write did not land. Failures are logged, never returned.
func recordHostFirewallDecision(disabled bool) {
	if err := writeHostFirewallDecision(disabled); err != nil {
		logx.As().Warn().Err(err).Bool("disabled", disabled).Msg(
			"could not record the host-firewall decision in runtime state; a later `block node reconfigure` " +
				"may not reflect it — re-run with --firewall-enabled to state the decision explicitly")
		return
	}
	logx.As().Debug().Bool("disabled", disabled).Msg("Recorded host-firewall decision into runtime state")
}

// writeHostFirewallDecision does the load-patch-flush, split out so the error
// path is testable without asserting on log output.
func writeHostFirewallDecision(disabled bool) error {
	sm, err := stateManager()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager")
	}
	if err := sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return errorx.IllegalState.Wrap(err, "failed to refresh state")
	}

	st := sm.State()
	fw := st.MachineState.Firewall
	if fw == nil {
		fw = &state.HostFirewallState{}
	}
	fw.Disabled = disabled
	st.MachineState.Firewall = fw

	if err := sm.Set(st).FlushState(); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to persist state")
	}
	return nil
}
