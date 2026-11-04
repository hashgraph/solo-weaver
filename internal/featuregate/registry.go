// SPDX-License-Identifier: Apache-2.0

package featuregate

import gatekit "github.com/automa-saga/gatekit"

// Catalog is solo-provisioner's feature registry: every gated behavior, keyed by
// the activation version at which its spec takes effect and tagged with its
// maturity. It is the single source of truth — CLI maturity labeling (#1392) and
// code-level behavior gates both resolve against it.
//
// Descriptions say only what the feature does; maturity and the activation
// version are the structured fields (Maturity, ActiveFrom), not prose.
//
// Spec semantics (gatekit): a feature is on when the last spec whose
// ActiveFrom <= the active version is default-on; an ALPHA spec additionally
// requires the gate-wide opt-in (WithExperimental, from --experimental) or a
// per-feature override (WithOverrides, from SOLO_PROVISIONER_ENABLE_<FEATURE>).
// A feature graduates by adding a later spec at a higher ActiveFrom with a more
// mature stage (e.g. ALPHA -> GA).
func Catalog() gatekit.Registry {
	return gatekit.Registry{
		// --- Activation version 1: the GA floor (shipped, default-on) ---
		NetworkPolicy: {
			Description: "Cilium network-policy management (`network policy`).",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 1, Default: true, Maturity: gatekit.GA},
			},
		},
		NetworkFirewall: {
			Description: "Declarative host firewall management (`network firewall`).",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 1, Default: true, Maturity: gatekit.GA},
			},
		},
		NetworkTrafficShaping: {
			Description: "tc/HTB traffic shaping for the block-node network (`network shape`).",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 1, Default: true, Maturity: gatekit.GA},
			},
		},
		EsoSecretManagement: {
			Description: "External-secrets operator and secret management (`eso`).",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 1, Default: true, Maturity: gatekit.GA},
			},
		},
		KubeOperatorManagement: {
			Description: "Manage the solo-operator lifecycle: install, uninstall, and upgrade (`kube operator`).",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 1, Default: true, Maturity: gatekit.GA},
			},
		},
		BlockNodeProvisionerManaged: {
			Description: "Manage block node lifecycle through solo-provisioner.",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 1, Default: true, Maturity: gatekit.GA},
			},
		},

		// --- Activation version 2: the operator-migration cohort ---
		// ALPHA: dormant until the axis reaches 2 AND the operator opts in via
		// --experimental (or a per-feature override). Each graduates to GA with a
		// later spec once validated.
		//
		// NOTE: block node has two named deployment models — BlockNodeProvisionerManaged
		// (direct Helm, GA, in the floor above) and BlockNodeOperatorManaged (via
		// solo-operator, Alpha). Code branches on the operator feature; the
		// provisioner model is the GA path it falls back to. The operator model
		// graduates by adding a later GA spec, at which point the provisioner model
		// can be marked Deprecated.
		BlockNodeOperatorManaged: {
			Description: "Manage block node lifecycle through solo-operator.",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 2, Default: true, Maturity: gatekit.Alpha},
			},
		},
		ConsensusNodeOperatorManaged: {
			Description: "Manage consensus node lifecycle through solo-operator.",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 2, Default: true, Maturity: gatekit.Alpha},
			},
		},
		ConsensusNodeMigrationSoak: {
			Description: "Soak-watch health during consensus migration (`consensus migration soak`).",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 2, Default: true, Maturity: gatekit.Alpha},
			},
		},
		SelfUpgrade: {
			Description: "Paired CLI/daemon self-upgrade with coordinated activation finalize.",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 2, Default: true, Maturity: gatekit.Alpha},
			},
		},
	}
}
