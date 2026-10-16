// SPDX-License-Identifier: Apache-2.0

package provenance

import (
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// Evaluate applies the outcome rules in docs/dev/bring-your-own-cluster.md
// section 5.2, in order, and returns the first that matches. It is pure: no I/O,
// no clock, no cluster. The errors it builds are decorated here because they
// cross a package boundary unchanged (they surface at the command's RunE), per
// the errx boundary rule.
//
// Rule ids match the design: P.1 (precondition), A.1-A.4, B.1-B.3, C.1-C.2.
func Evaluate(in Input) Outcome {
	// Precondition (all branches): a recorded HostIdentity that no longer matches
	// means the state directory belongs to another machine.
	if in.RecordedMachineIDPresent && !in.MachineIDMatches {
		return stop("P.1", errorx.IllegalState.New("state belongs to another machine"),
			reasons.PreconditionNotMet,
			"This state directory was provisioned on a different machine (its recorded machine ID does not match /etc/machine-id).",
			"Do not copy /opt/solo/weaver/state between machines; provision this host from scratch.")
	}

	switch {
	case in.HasProvisionRecord && in.TargetMatchesProvisionRecord:
		return branchA(in)
	case !in.HasProvisionRecord && in.LocalEvidencePasses:
		return branchB(in)
	default:
		return branchC(in)
	}
}

// branchA: the target matches the ProvisionRecord.
func branchA(in Input) Outcome {
	switch {
	case in.LocalEvidenceFailed || !in.APIReachable:
		// A.1 - never fall back to external for our own, broken cluster.
		return stop("A.1", errorx.IllegalState.New("local cluster evidence failed: %s", piece(in)),
			reasons.PreconditionNotMet,
			"This is this host's own cluster but it is not healthy; weaver will not treat it as external.",
			"Repair the failing piece, for example: systemctl status kubelet crio.")
	case in.LocalEvidenceUnreadable:
		// A.2 - cannot read a check (typically no root).
		return stop("A.2", errorx.ExternalError.New("cannot read local cluster evidence: %s", piece(in)),
			reasons.PermissionDenied,
			"A local check could not be read; weaver does not treat 'cannot read' as 'not ours'.",
			"Re-run with sudo.")
	case in.LiveClusterID == in.ProvisionedClusterID:
		// A.3 - healthy and the live cluster is the one we recorded.
		return pass("A.3", Provisioned)
	default:
		// A.4 - the cluster was rebuilt; the recorded state is stale.
		return stop("A.4", errorx.IllegalState.New(
			"cluster was rebuilt: recorded cluster ID %s but live cluster ID %s", in.ProvisionedClusterID, in.LiveClusterID),
			reasons.PreconditionNotMet,
			"The recorded state belongs to a cluster that no longer exists on this host.",
			"Review and clean up state, then re-run 'solo-provisioner kube cluster install'.")
	}
}

// branchB: no ProvisionRecord yet, but local evidence passes (older install, TOFU).
func branchB(in Input) Outcome {
	switch {
	case !in.APIReachable:
		// B.1 - cannot read the live cluster ID.
		return stop("B.1", errorx.IllegalState.New("cluster API unreachable: cannot read the cluster ID"),
			reasons.PreconditionNotMet,
			"This host looks provisioned but the cluster ID cannot be read while the API is down.",
			"Start the cluster, or run 'solo-provisioner kube cluster connect' once it is up.")
	case in.ExternalStateExists:
		// B.2 - the same cluster ID is already described under clusters/<uid>.
		return stop("B.2", errorx.IllegalState.New("cluster already has external state; it is never adopted silently"),
			reasons.PreconditionNotMet,
			"This cluster already has state under clusters/<uid>; weaver will not merge or pick one automatically.",
			"Discard the external copy, or adopt it into hosts/local (see design section 5.2).")
	default:
		// B.3 - record the ProvisionRecord on first use.
		return pass("B.3", Provisioned)
	}
}

// branchC: the target is not this host's cluster.
func branchC(in Input) Outcome {
	if in.KubeconfigGiven {
		// C.1 - external. Registration is handled by the resolver; the gate only
		// classifies.
		return pass("C.1", External)
	}
	// C.2 - nothing usable here and no kubeconfig.
	return stop("C.2", errorx.IllegalState.New("no cluster for this command on this host"),
		reasons.NotInstalled,
		"Nothing is installed here and no external cluster was given.",
		"Run 'solo-provisioner kube cluster install', or pass --kubeconfig for an external cluster.")
}

func piece(in Input) string {
	if in.FailingPiece != "" {
		return in.FailingPiece
	}
	return "unknown check"
}

func pass(rule string, origin Origin) Outcome {
	return Outcome{Origin: origin, Rule: rule}
}

func stop(rule string, err error, reason errx.Reason, hints ...string) Outcome {
	return Outcome{
		Rule: rule,
		Err:  errx.Decorate(err, reason, hints...),
	}
}
