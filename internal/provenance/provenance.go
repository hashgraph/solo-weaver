// SPDX-License-Identifier: Apache-2.0

// Package provenance implements the cluster provenance gate described in
// docs/dev/bring-your-own-cluster.md (section 5). The gate decides which cluster
// a command is about and whether this host provisioned it, before any real work
// runs. It establishes the provenance of the target so state and reality can be
// trusted; it is not a security boundary.
//
// Vocabulary (each noun is one thing, never the bare word "identity"):
//   - HostIdentity   — the machine, /etc/machine-id.
//   - ClusterIdentity — the cluster, the kube-system UID (+ server + CA).
//   - ProvisionRecord — what this host recorded about the cluster it provisioned
//     (its ClusterIdentity, bound to this HostIdentity), in hosts/local/host.yaml.
//   - Target         — the cluster a command is pointed at.
//   - Origin         — the verdict: provisioned or external. A refusal ("stop")
//     is an error, not an Origin.
//
// Evaluate is the pure outcome table (section 5.2); Gate applies a command's
// declared Need (section 5.3); osCollector (collector.go) gathers the real
// evidence. Two inputs are not yet wired and are marked TODO(#1235) in the
// collector: the ProvisionRecord and the deep binary-provenance checks.
package provenance

// Origin is the gate's verdict for a target cluster that is safe to act on. A
// refusal is represented by a non-nil Outcome.Err, not by an Origin value, so an
// Origin is always a genuine operating mode. The string form matches the on-disk
// state field (design section 6): "provisioned" / "external".
type Origin int

const (
	// originUnset is the zero value, used on a refusal where no Origin applies.
	originUnset Origin = iota
	// Provisioned — the target is a cluster this host provisioned; state lives
	// under hosts/local.
	Provisioned
	// External — the target is a cluster weaver did not provision; state lives
	// under clusters/<uid>. API-only work is allowed.
	External
)

func (o Origin) String() string {
	switch o {
	case Provisioned:
		return "provisioned"
	case External:
		return "external"
	default:
		return "unset"
	}
}

// Need is what a command declares it requires from the gate (design section 5.3).
// NeedHostAPI is wired into block node install; the others are defined so the
// mapping is complete and testable as more commands adopt the gate.
type Need int

const (
	// NeedNone - no cluster and no host evidence (version, help, ...).
	NeedNone Need = iota
	// NeedHost - host-level work that must also run on a broken cluster.
	NeedHost
	// NeedAPI - API work in either provisioned or external mode.
	NeedAPI
	// NeedHostAPI - provisioned-only work that also needs the cluster
	// (e.g. block node install on a provisioned host).
	NeedHostAPI
)

func (n Need) String() string {
	switch n {
	case NeedHost:
		return "host"
	case NeedAPI:
		return "api"
	case NeedHostAPI:
		return "host+api"
	default:
		return "none"
	}
}

// ParseNeed is the inverse of Need.String. An unrecognised value is NeedNone, so
// a command with no declaration is never accidentally gated.
func ParseNeed(s string) Need {
	switch s {
	case "host":
		return NeedHost
	case "api":
		return NeedAPI
	case "host+api":
		return NeedHostAPI
	default:
		return NeedNone
	}
}

// Input is the full set of decision inputs the gate evaluates. It is deliberately
// flat and side-effect free so the outcome table (section 5.2) can be exercised
// exhaustively in a table test. A Collector (collector.go) fills it from the
// machine, the ProvisionRecord and the live cluster.
type Input struct {
	// --- precondition (P.1) ---

	// RecordedMachineIDPresent is true when host.yaml records a machine ID
	// (the HostIdentity).
	RecordedMachineIDPresent bool
	// MachineIDMatches is true when the recorded HostIdentity equals this
	// machine's. Only consulted when RecordedMachineIDPresent is true.
	MachineIDMatches bool

	// --- branch selection ---

	// HasProvisionRecord is true when host.yaml records a provisioned cluster
	// (its ClusterIdentity: cluster ID / server / CA fingerprint).
	HasProvisionRecord bool
	// TargetMatchesProvisionRecord is true when the Target matches the
	// ProvisionRecord (CA fingerprint; server address only as a fallback).
	TargetMatchesProvisionRecord bool
	// LocalEvidencePasses is true when every local evidence check passed
	// (binaries, running processes, admin.conf, address). Section 5.1.
	LocalEvidencePasses bool

	// --- branch A refinements ---

	// LocalEvidenceFailed is true when a local check was read and did not match.
	LocalEvidenceFailed bool
	// LocalEvidenceUnreadable is true when a local check could not be read
	// (for example /proc needs root).
	LocalEvidenceUnreadable bool
	// FailingPiece names the failing or unreadable check, for the hint.
	FailingPiece string

	// --- live cluster ---

	// APIReachable is true when the cluster API answered.
	APIReachable bool
	// LiveClusterID is the kube-system UID (ClusterIdentity) of the live cluster.
	LiveClusterID string
	// ProvisionedClusterID is the ClusterIdentity recorded in the ProvisionRecord.
	ProvisionedClusterID string

	// --- branch B refinement ---

	// ExternalStateExists is true when clusters/<live-uid>/ already holds state.
	ExternalStateExists bool

	// --- branch C refinement ---

	// KubeconfigGiven is true when --kubeconfig was passed.
	KubeconfigGiven bool
}

// Outcome is the result of Evaluate and Gate. On success Err is nil and Origin is
// the resolved operating mode; on a refusal Err is the decorated error to return
// and Origin is unset. Rule is the rule id that fired (for example "A.4"), for
// diagnostics in both cases.
type Outcome struct {
	Origin Origin
	Rule   string
	Err    error
}

// Stopped reports whether the outcome is a refusal.
func (o Outcome) Stopped() bool { return o.Err != nil }
