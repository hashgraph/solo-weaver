// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"fmt"

	"github.com/automa-saga/automa"
	bnpkg "github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/internal/daemon"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
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
//     The exception is removeShaping, for shaping that nobody chose.
//
// It is a thin adapter over workflows.NetworkPlaneSteps — the single assembler
// also used by the install path (via NetworkSetupWorkflow) — that maps the
// block-node inputs onto the shared options and always requests the inline daemon
// reload (both reconfigure and upgrade may run against a live daemon whose
// daemon.yaml changed). See workflows.NetworkPlaneSteps for the step semantics.
//
// trafficShapingEnabled is the resolved traffic-shaping target. force is the
// operator's --force. removeShaping tears the shaping bundle down without
// allowTeardown's host-firewall teardown. ports are the block-node facility
// ports resolved by resolveBlockNodePorts.
func networkPlaneSteps(ins models.BlockNodeInputs, force, trafficShapingEnabled, allowTeardown, removeShaping bool, ports workflows.BlockNodePorts) []automa.Builder {
	return workflows.NetworkPlaneSteps(workflows.NetworkPlaneOptions{
		Force:                 force,
		TrafficShapingEnabled: trafficShapingEnabled,
		Ports:                 ports,
		Namespace:             ins.Namespace,
		Statusz:               daemon.StatuszConfig{BaseURL: ins.StatuszBaseURL, PollInterval: ins.StatuszPollInterval},
		EgressInterface:       ins.EgressInterface,
		LinkRate:              ins.LinkRate,
		ShapeOverrides:        toClassOverrides(ins.ShapeOverrides),
		AllowTeardown:         allowTeardown,
		RemoveShaping:         removeShaping,
		WithDaemonReload:      true,
	})
}

// releaseValuesReader returns the deployed block node release's user-supplied
// values, or nil when there is no release.
type releaseValuesReader func(ins models.BlockNodeInputs) (map[string]any, error)

func readDeployedReleaseValues(ins models.BlockNodeInputs) (map[string]any, error) {
	m, err := bnpkg.NewManager(ins)
	if err != nil {
		return nil, err
	}
	return m.GetReleaseValues()
}

// resolveBlockNodePorts reads the health and publish ports from the operator's
// effective --values, so the bn-health and bn-publisher policies track the ports
// the BN actually listens on; each falls back to the chart default when the
// operator supplies no override.
//
// When the chart upgrade reuses the release's values, a publish port set on the
// release survives a --values file that omits it, so it is read from the release
// too. readRelease is nil on install, which never reuses values.
func resolveBlockNodePorts(ins models.BlockNodeInputs, readRelease releaseValuesReader) (workflows.BlockNodePorts, error) {
	health, err := bnpkg.ResolveHealthPort(ins.ValuesFile)
	if err != nil {
		return workflows.BlockNodePorts{}, err
	}
	var released map[string]any
	if ins.ReuseValues && readRelease != nil {
		if released, err = readRelease(ins); err != nil {
			return workflows.BlockNodePorts{}, errorx.Decorate(err, "failed to read the deployed block node release values").
				WithProperty(models.ErrPropertyResolution,
					fmt.Sprintf("check the release with 'helm get values %s -n %s', "+
						"or pass --no-reuse-values with a --values file that sets the publish port", ins.Release, ins.Namespace))
		}
	}
	publisher, err := bnpkg.ResolvePublisherPort(ins.ValuesFile, released)
	if err != nil {
		return workflows.BlockNodePorts{}, err
	}
	return workflows.BlockNodePorts{Health: health, Publisher: publisher}, nil
}
