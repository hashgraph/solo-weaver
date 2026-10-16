// SPDX-License-Identifier: Apache-2.0

package provenance

import (
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// Gate applies a command's declared Need to a resolved Input and returns the
// final Outcome. The full outcome table (Evaluate) runs only for the needs that
// reach the live cluster (api, host+api); host does a reduced, offline check,
// and none always passes. Design section 5.3.
func Gate(need Need, in Input) Outcome {
	switch need {
	case NeedNone:
		return pass("none", Provisioned)

	case NeedHost:
		// Offline check only: HostIdentity matches (P.1) and the run is not
		// external. It never reads the API, so a down or unhealthy local cluster
		// does not stop it, and branches A/B/C are not evaluated.
		if in.RecordedMachineIDPresent && !in.MachineIDMatches {
			return stop("P.1", errorx.IllegalState.New("state belongs to another machine"),
				reasons.PreconditionNotMet,
				"This state directory was provisioned on a different machine.",
				"Do not copy the state directory between machines.")
		}
		if in.KubeconfigGiven {
			return stop("host", errorx.IllegalState.New("host-level command cannot run against an external cluster"),
				reasons.PreconditionNotMet,
				"This command does host-level work and does not apply to an external cluster.",
				"Run it on the provisioned host without --kubeconfig.")
		}
		return pass("host", Provisioned)

	case NeedAPI:
		// Either provisioned or external is acceptable.
		return Evaluate(in)

	case NeedHostAPI:
		// Provisioned-only work that also needs the cluster. External is a stop.
		out := Evaluate(in)
		if !out.Stopped() && out.Origin == External {
			return stop("host+api", errorx.IllegalState.New("this command needs a provisioned host, but the target is external"),
				reasons.PreconditionNotMet,
				"Block node install runs host steps (firewall, shaping, storage) that only apply to a cluster weaver provisioned on this host.",
				"Run it on the provisioned host, or use an API-only command for an external cluster.")
		}
		return out

	default:
		return stop("internal", errorx.AssertionFailed.New("unknown need: %d", need), reasons.Internal)
	}
}
