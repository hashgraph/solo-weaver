// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// stateManager is the seam over the production state manager so command tests
// can substitute one backed by a temp state file.
var stateManager = func() (state.Manager, error) { return state.NewStateManager() }

// applyHostFirewallChange runs apply, the nft change, while holding the
// firewall component's lock, then records the operator's enable/disable
// decision into MachineState.Firewall so the standalone `network firewall`
// verbs and the block-node workflow share one source of truth.
//
// Without it the two paths disagree: `block node reconfigure` seeds its
// enable/disable choice from the persisted decision, which the standalone verbs
// never wrote, so a firewall created here read as "never configured" — and a
// later no-flag reconfigure resolved that to disabled and deleted it (issue
// #1003). The inverse matters just as much: after `delete --all`, a recorded
// decision still saying "enabled" would make the next reconfigure re-create
// the firewall the operator just removed.
//
// Only the decision is written. The CIDR/port content stays in the firewall's
// own config file, which ResolveHostFirewallConfig reads directly as a tier
// above the recorded state — mirroring it here would have to squeeze a Rule's
// port specs into HostConfig's []int and would silently drop any inclusive range.
//
// The lock is taken before apply, so a command that could not record its
// decision fails before touching the kernel. A record that still fails after
// apply is returned, never only logged: a dropped decision is what makes the
// next reconfigure undo this command.
func applyHostFirewallChange(disabled bool, apply func() error) error {
	sm, err := stateManager()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create state manager")
	}

	applied := false
	err = state.RunOwning(sm, []state.ComponentID{state.ComponentFirewall},
		func() error {
			if err := apply(); err != nil {
				return err
			}
			applied = true
			return nil
		},
		func(st *state.State) error {
			fw := st.MachineState.Firewall
			if fw == nil {
				fw = &state.HostFirewallState{}
			}
			fw.Disabled = disabled
			st.MachineState.Firewall = fw
			return nil
		})
	if err != nil && applied {
		return errx.Decorate(
			errorx.IllegalState.Wrap(err, "the host firewall change was applied but the decision was not recorded"),
			reasons.StateOperationFailed,
			"Re-run the same command; it is safe to repeat and records the decision.")
	}
	return err
}
