// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// networkPlaneSteps builds the static network-plane step list shared by
// `block node reconfigure` and `block node upgrade`. Both converge the host
// firewall and the BN traffic-shaping bundle (policy plane + tc HTB shaping +
// daemon traffic-shaper monitor) to a desired target, differing only in how that
// target is resolved and whether teardown is permitted:
//
//   - reconfigure resolves the target from the operator (prompt/flags) and passes
//     allowTeardown=true, so turning a feature off deletes it.
//   - upgrade resolves the target from persisted state and passes
//     allowTeardown=false: it re-asserts (create-if-missing) whatever the install
//     decision was, but never tears anything down from a routine version bump.
//
// It is a thin adapter over workflows.NetworkPlaneSteps — the single assembler
// also used by the install path (via NetworkSetupWorkflow) — that maps the
// block-node inputs onto the shared options and always requests the inline daemon
// reload (both reconfigure and upgrade may run against a live daemon whose
// daemon.yaml changed). See workflows.NetworkPlaneSteps for the step semantics.
//
// trafficShapingEnabled is the resolved traffic-shaping target. force is the
// operator's --force. healthPort is the resolved block-node health/statusz port.
func networkPlaneSteps(ins models.BlockNodeInputs, force, trafficShapingEnabled, allowTeardown bool, healthPort string) []automa.Builder {
	return workflows.NetworkPlaneSteps(workflows.NetworkPlaneOptions{
		Force:                 force,
		TrafficShapingEnabled: trafficShapingEnabled,
		HealthPort:            healthPort,
		Namespace:             ins.Namespace,
		EgressInterface:       ins.EgressInterface,
		LinkRate:              ins.LinkRate,
		ShapeOverrides:        toClassOverrides(ins.ShapeOverrides),
		AllowTeardown:         allowTeardown,
		WithDaemonReload:      true,
	})
}
