// SPDX-License-Identifier: Apache-2.0

// Package featuregate is solo-provisioner's thin adapter over
// github.com/automa-saga/gatekit. It owns the concrete feature catalog, the
// activation-version axis, and the wiring of the operator-facing opt-ins
// (--experimental, SOLO_PROVISIONER_ENABLE_<FEATURE>) into gatekit's gate.
//
// SPIKE (epic #1275): this is Layer 1 only — a version-keyed + maturity gate.
// There is no finalize, no state-backed activation axis, and no migration
// gating here yet; those are Layer 2 and remain the epic's later tickets. The
// VersionSource is a constant for now (see CurrentActivationVersion).
package featuregate

import gatekit "github.com/automa-saga/gatekit"

// Feature constants. A feature names a gated *behavior*, namespaced by the
// command area it lives under — not a command itself. Most stable commands need
// no entry here; they are the GA floor.
const (
	// BlockNodeProvisionerManaged is the direct-Helm block-node deployment model
	// managed by solo-provisioner itself — today's GA default.
	BlockNodeProvisionerManaged gatekit.Feature = "BlockNodeProvisionerManaged"

	// Not yet in main — uncommented by their implementing PR (see registry.go):
	//   BlockNodeOperatorManaged (operator HelmCapsule path) -> PR #1364.
	// BlockNodeOperatorManaged gatekit.Feature = "BlockNodeOperatorManaged"

	// ConsensusNodeOperatorManaged gates consensus-node lifecycle via solo-operator
	// against the legacy path.
	ConsensusNodeOperatorManaged gatekit.Feature = "ConsensusNodeOperatorManaged"

	// KubeOperatorManagement covers the solo-operator lifecycle managed by
	// solo-provisioner: install, uninstall, and upgrade (`kube operator ...`).
	KubeOperatorManagement gatekit.Feature = "KubeOperatorManagement"

	// NetworkTrafficShaping gates the tc/HTB traffic shaper (`network shape`).
	NetworkTrafficShaping gatekit.Feature = "NetworkTrafficShaping"

	// NetworkPolicy gates Cilium network-policy management (`network policy`).
	NetworkPolicy gatekit.Feature = "NetworkPolicy"

	// NetworkFirewall gates declarative host firewall management
	// (`network firewall`).
	NetworkFirewall gatekit.Feature = "NetworkFirewall"

	// EsoSecretManagement gates the external-secrets operator and secret
	// handling (`eso`).
	EsoSecretManagement gatekit.Feature = "EsoSecretManagement"

	// Not yet in main — uncommented by their implementing PR (see registry.go):
	//   ConsensusNodeMigrationSoak (`consensus migration soak` watcher).
	//   SelfUpgrade (paired CLI/daemon self-upgrade, #500).
	// ConsensusNodeMigrationSoak gatekit.Feature = "ConsensusNodeMigrationSoak"
	// SelfUpgrade                gatekit.Feature = "SelfUpgrade"
)

// MaturityOf returns the declared maturity of a feature — the maturity of its
// latest spec — or empty if the feature is not in the catalog. It is static
// (registry-derived), so command/flag labelling can read it without building a
// gate.
func MaturityOf(f gatekit.Feature) gatekit.Maturity {
	def, ok := Catalog()[f]
	if !ok || len(def.Specs) == 0 {
		return ""
	}
	return def.Specs[len(def.Specs)-1].Maturity
}

// ActiveFromOf returns the activation version a feature first turns on at (its
// first spec's ActiveFrom), or 0 if the feature is not in the catalog.
func ActiveFromOf(f gatekit.Feature) gatekit.Activation {
	def, ok := Catalog()[f]
	if !ok || len(def.Specs) == 0 {
		return 0
	}
	return def.Specs[0].ActiveFrom
}

// CurrentActivationVersion is the activation version in force for this binary —
// the cursor that decides which specs are live. Everything with
// ActiveFrom <= this value and a default-on, opted-in spec resolves on.
//
// Think of each integer as a roadmap milestone (documented per feature in the
// catalog): 1 is today's GA floor; 2 is the operator-migration cohort.
//
// SPIKE: this is a constant. Layer 2 replaces it with a state-backed,
// finalize-advanced VersionSource (RSL effective-value layer).
const CurrentActivationVersion gatekit.Activation = 1

// SupportWindow is the support window W for the activation axis: how many prior
// activation versions this binary still supports in addition to the max
// (gatekit WithSupportWindow; see docs/dev/feature-gates.md). It is the single
// source of truth for W — the gate (New) and the registry lints (Lint, Baseline)
// MUST use the same value, or the lints check a different window than the gate
// enforces. W=1 is the strict N/N+1 window.
const SupportWindow = 1
