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

	// BlockNodeOperatorManaged gates the solo-operator (HelmCapsule) deployment
	// model for block nodes against the provisioner-managed one.
	BlockNodeOperatorManaged gatekit.Feature = "BlockNodeOperatorManaged"

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

	// ConsensusNodeMigrationSoak gates the migration soak watcher
	// (`consensus migration soak`).
	ConsensusNodeMigrationSoak gatekit.Feature = "ConsensusNodeMigrationSoak"

	// EsoSecretManagement gates the external-secrets operator and secret
	// handling (`eso`).
	EsoSecretManagement gatekit.Feature = "EsoSecretManagement"

	// SelfUpgrade gates paired daemon self-upgrade / auto-finalize (#500).
	SelfUpgrade gatekit.Feature = "SelfUpgrade"
)

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
