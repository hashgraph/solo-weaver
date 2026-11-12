// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

const ciliumCLI = "cilium"

// Seams so tests run no workflow and need no binaries on disk.
var (
	hostBinaryAt      = steps.HostBinaryAt
	ciliumCLIWorkflow = workflows.UpgradeCiliumCLIWorkflow
	ciliumHopWorkflow = workflows.CiliumHopWorkflow
)

// CiliumHopChecks checks that hop can run on the cluster in shape. It reads
// nothing and changes nothing. The live health of Cilium is checked by the
// first step of the hop.
func CiliumHopChecks(shape rsl.ClusterShape, m Matrix, hop Hop) (Checks, error) {
	if hop.Kind != KindCilium || (hop.Step != StepPatch && hop.Step != StepMinor) {
		return nil, errorx.IllegalArgument.New("%s %s is not a Cilium patch or minor hop", hop.Kind, hop.Step)
	}
	from, err := parseVersion(hop.From)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "hop start")
	}
	to, err := parseVersion(hop.To)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "hop target")
	}
	c, err := m.compile()
	if err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "invalid upgrade matrix")
	}

	var cs Checks
	cs.add("the cluster was read", shape.Observed, "the cluster could not be fully read")
	cs.add("one node", !shape.Kubernetes.UnexpectedNodes,
		fmt.Sprintf("the cluster has %d nodes; upgrades support one", shape.Kubernetes.NodeCount))
	cs.add("Cilium is where the hop starts", shape.Cilium.Chart == hop.From,
		fmt.Sprintf("the Cilium chart is %q, the hop starts at %s", shape.Cilium.Chart, hop.From))
	cs.add("the Cilium agent matches its chart", shape.Cilium.Agent != "" && !shape.Cilium.Mismatch,
		fmt.Sprintf("the agent runs %q and the chart is %q", shape.Cilium.Agent, shape.Cilium.Chart))

	rung, shipped := c.ciliumRung(to.minorOf())
	shipped = shipped && rung.v == to
	cs.add("the target is a catalog version", shipped, fmt.Sprintf("Cilium %s is not a version this binary ships", to))

	oneStep := from.less(to) && (from.minorOf() == to.minorOf() || from.minorOf().next() == to.minorOf())
	cs.add("one step at a time", oneStep, fmt.Sprintf("%s -> %s is not a patch or one minor up", from, to))

	kube, err := parseVersion(shape.Kubernetes.ControlPlane)
	cs.add("the target runs on this Kubernetes", err == nil && shipped && rung.supports(kube.minorOf()),
		fmt.Sprintf("Cilium %s does not support Kubernetes %q", to, shape.Kubernetes.ControlPlane))
	return cs, nil
}

// ApplyCiliumHop moves the running Cilium by one hop. It reads the cluster,
// does nothing when the hop is already applied, refuses when a check fails,
// puts the cilium CLI on the catalog default, then runs the hop and records
// the new cluster. Each part holds the state locks it writes and leaves an
// entry in the action history.
func ApplyCiliumHop(ctx context.Context, src LiveSources, run WorkflowRunner, hop Hop) error {
	if run == nil {
		return errorx.IllegalArgument.New("a workflow runner must be set")
	}
	shape, m, err := readLive(ctx, src)
	if err != nil {
		return err
	}
	if shape.Cilium.Chart == hop.To && shape.Cilium.Agent == hop.To {
		logx.As().Info().Str("cilium", hop.To).Msg("Cilium already runs the hop's target; nothing to do")
		return nil
	}
	checks, err := CiliumHopChecks(shape, m, hop)
	if err != nil {
		return err
	}
	if err := checks.Err(); err != nil {
		return err
	}

	cat, err := src.Catalog()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to load the catalog")
	}
	cli, err := cat.GetHostArtifact(ciliumCLI)
	if err != nil {
		return errorx.IllegalFormat.Wrap(err, "catalog")
	}
	version := string(cli.Default)
	at, err := hostBinaryAt(ciliumCLI, version)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to read the installed cilium CLI")
	}
	if !at {
		err := recordHop(ctx, src, map[string]string{"component": "cilium-cli", "to": version},
			[]state.ComponentID{state.ComponentSystem},
			func() error { return run(ctx, ciliumCLIWorkflow(version)) },
			func(st *state.State) {
				if st.MachineState.Software == nil {
					st.MachineState.Software = map[string]state.SoftwareState{}
				}
				sw := st.MachineState.Software[ciliumCLI]
				sw.Name, sw.Version, sw.Installed = ciliumCLI, version, true
				st.MachineState.Software[ciliumCLI] = sw
			})
		if err != nil {
			return err
		}
	}

	return recordHop(ctx, src, map[string]string{"component": "cilium", "from": hop.From, "to": hop.To},
		[]state.ComponentID{state.ComponentCluster},
		func() error { return run(ctx, ciliumHopWorkflow(hop.From, hop.To)) },
		nil)
}
