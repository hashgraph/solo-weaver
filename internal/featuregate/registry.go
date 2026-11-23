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
// Spec semantics (gatekit): a feature is on when the last spec with
// ActiveFrom <= the active version is default-on. An ALPHA spec is off until the
// operator opts in (--experimental or the SOLO_PROVISIONER_ENABLE_<FEATURE> env var)
// AND it is ready (Default:true); an Alpha,Default:false spec is DORMANT — unfinished
// and unreachable by any opt-in until flipped to Default:true or graduated. A feature
// graduates in place: raise its Maturity (ALPHA -> GA) at the same ActiveFrom.
func Catalog() gatekit.Registry {
	return gatekit.Registry{
		// The catalog holds only genuinely gated behaviours. A stable (GA) capability
		// is not a gate — nothing branches on it and it always runs — so it has no
		// entry here; it is discoverable through `--help`, not `feature-gate list`.
		//
		// --- Activation version 1: the alpha opt-in cohort (dev gates) ---
		// ALPHA dev gates live at the CURRENT activation version (1), not a future
		// one: solo does not need rolling-upgrade coordinated activation, so a dev
		// gate never raises the max (see docs/dev/feature-gates.md and gatekit-spec
		// Lesson 7). Each is off by default and, being Default:true, is dogfoodable
		// via --experimental (or its env var); set Default:false to keep an
		// unfinished feature dormant (unreachable by any opt-in). Each graduates to
		// GA with a later spec once validated.
		ConsensusNodeOperatorManaged: {
			Description: "Manage consensus node lifecycle through solo-operator.",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 1, Default: true, Maturity: gatekit.Alpha},
			},
		},
		KubeClusterUpgrade: {
			Description: "Upgrade the Kubernetes cluster along the k8s/Cilium ladder (`kube cluster upgrade`).",
			Specs: gatekit.VersionedSpecs{
				{ActiveFrom: 1, Default: true, Maturity: gatekit.Alpha},
			},
		},

		// Not yet in main. A feature enters the registry in the same PR that lands
		// the code it gates, so the catalog never advertises a gate with no
		// behavior behind it. When a feature lands incrementally (code merged before
		// it is ready to dogfood), register it Default:false — dormant, so no opt-in
		// can switch the half-done path on — and flip to Default:true once ready.
		// Uncomment each (and its constant in features.go) in its implementing PR:
		//   - BlockNodeOperatorManaged (operator HelmCapsule path) -> PR #1364.
		//     Block node has two deployment models: BlockNodeProvisionerManaged
		//     (direct Helm, GA, in the floor above) is today's default; the
		//     operator model is the Alpha alternative the gate selects, with the
		//     provisioner model as the fallback.
		//   - ConsensusNodeMigrationSoak (`consensus migration soak` watcher).
		//   - SelfUpgrade (paired CLI/daemon self-upgrade, #500).
		//
		// BlockNodeOperatorManaged: {
		// 	Description: "Manage block node lifecycle through solo-operator.",
		// 	Specs: gatekit.VersionedSpecs{
		// 		{ActiveFrom: 1, Default: false, Maturity: gatekit.Alpha},
		// 	},
		// },
		// ConsensusNodeMigrationSoak: {
		// 	Description: "Soak-watch health during consensus migration (`consensus migration soak`).",
		// 	Specs: gatekit.VersionedSpecs{
		// 		{ActiveFrom: 1, Default: false, Maturity: gatekit.Alpha},
		// 	},
		// },
		// SelfUpgrade: {
		// 	Description: "Paired CLI/daemon self-upgrade with coordinated activation finalize.",
		// 	Specs: gatekit.VersionedSpecs{
		// 		{ActiveFrom: 1, Default: false, Maturity: gatekit.Alpha},
		// 	},
		// },
	}
}
