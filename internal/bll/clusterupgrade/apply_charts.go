// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"fmt"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

var chartHopWorkflow = workflows.ChartHopWorkflow

// ChartsHopChecks checks that hop can run on the cluster in shape. It reads
// nothing and changes nothing; whether a chart suits the running Kubernetes is
// checked by the hop's first step.
func ChartsHopChecks(shape rsl.ClusterShape, m Matrix, hop Hop) (Checks, error) {
	if hop.Kind != KindCharts || (hop.Step != StepCoupled && hop.Step != StepRefresh) {
		return nil, errorx.IllegalArgument.New("%s %s is not a coupled or refresh charts hop", hop.Kind, hop.Step)
	}
	if len(hop.Charts) == 0 {
		return nil, errorx.IllegalArgument.New("a charts hop must name at least one chart")
	}
	seen := map[string]bool{}
	for _, b := range hop.Charts {
		if seen[b.Chart] {
			return nil, errorx.IllegalArgument.New("chart %s appears twice in the hop", b.Chart)
		}
		seen[b.Chart] = true
		if _, err := parseVersion(b.From); err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "%s start", b.Chart)
		}
		if _, err := parseVersion(b.To); err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "%s target", b.Chart)
		}
	}
	var kubeMinor minorVersion
	if hop.Step == StepCoupled {
		var err error
		if kubeMinor, err = parseMinor(hop.Kubernetes); err != nil {
			return nil, errorx.IllegalArgument.Wrap(err, "the Kubernetes minor of a coupled hop")
		}
	}
	c, err := m.compile()
	if err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "invalid upgrade matrix")
	}

	var cs Checks
	cs.add("the cluster was read", shape.Observed, "the cluster could not be fully read")
	cs.add("one node", !shape.Kubernetes.UnexpectedNodes,
		fmt.Sprintf("the cluster has %d nodes; upgrades support one", shape.Kubernetes.NodeCount))

	// A chart already at its target counts as started, so a hop that stopped
	// part way can run again.
	var elsewhere, notShipped, noRecipe []string
	rung, rungOK := c.kubeRung(kubeMinor)
	for _, b := range hop.Charts {
		ch := shape.Charts[b.Chart]
		if ch.Missing || ch.Identity != rsl.IdentityCurrent || (ch.Live != b.From && ch.Live != b.To) {
			elsewhere = append(elsewhere, fmt.Sprintf("%s runs %q, the hop starts at %s", b.Chart, ch.Live, b.From))
		}
		want := ""
		switch hop.Step {
		case StepRefresh:
			if r, ok := c.charts[b.Chart]; ok {
				want = r.def.String()
			}
		case StepCoupled:
			if rungOK {
				if v, ok := rung.charts[b.Chart]; ok {
					want = v.String()
				}
			}
		}
		if want != b.To {
			notShipped = append(notShipped, fmt.Sprintf("%s %s", b.Chart, b.To))
		}
		if !steps.ChartHasRecipe(b.Chart) {
			noRecipe = append(noRecipe, b.Chart)
		}
	}
	cs.add("every chart is where the hop starts", len(elsewhere) == 0, strings.Join(elsewhere, "; "))
	cs.add("every target is the catalog's", len(notShipped) == 0,
		fmt.Sprintf("not the %s target this binary ships: %s", hop.Step, strings.Join(notShipped, ", ")))
	cs.add("every chart can be upgraded", len(noRecipe) == 0,
		"this binary does not upgrade "+strings.Join(noRecipe, ", "))

	if hop.Step == StepCoupled {
		cp, err := parseVersion(shape.Kubernetes.ControlPlane)
		ok := err == nil && (cp.minorOf() == kubeMinor || cp.minorOf().next() == kubeMinor)
		cs.add("the hop is for this cluster's Kubernetes", ok,
			fmt.Sprintf("the bumps are for Kubernetes %s and the cluster runs %q", kubeMinor, shape.Kubernetes.ControlPlane))
	}
	return cs, nil
}

// ApplyChartsHop runs one charts hop: a coupled bump or the charts refresh. It
// reads the cluster, does nothing when every chart already runs its target,
// refuses when a check fails, then runs the hop under the cluster lock,
// records the cluster and leaves an entry in the action history.
func ApplyChartsHop(ctx context.Context, src LiveSources, run WorkflowRunner, hop Hop) error {
	if run == nil {
		return errorx.IllegalArgument.New("a workflow runner must be set")
	}
	shape, m, err := readLive(ctx, src)
	if err != nil {
		return err
	}
	checks, err := ChartsHopChecks(shape, m, hop)
	if err != nil {
		return err
	}
	done := true
	for _, b := range hop.Charts {
		if ch := shape.Charts[b.Chart]; ch.Identity != rsl.IdentityCurrent || ch.Live != b.To {
			done = false
		}
	}
	if done {
		logx.As().Info().Str("step", string(hop.Step)).Msg("Every chart already runs the hop's target; nothing to do")
		return nil
	}
	if err := checks.Err(); err != nil {
		return err
	}

	bumps := make([]steps.ChartBump, len(hop.Charts))
	moves := make([]string, len(hop.Charts))
	for i, b := range hop.Charts {
		bumps[i] = steps.ChartBump{Chart: b.Chart, From: b.From, To: b.To}
		moves[i] = fmt.Sprintf("%s=%s->%s", b.Chart, b.From, b.To)
	}
	inputs := map[string]string{"component": "charts", "step": string(hop.Step), "charts": strings.Join(moves, ",")}
	if hop.Kubernetes != "" {
		inputs["kubernetes"] = hop.Kubernetes
	}
	return recordHop(ctx, src, inputs, []state.ComponentID{state.ComponentCluster},
		func() error { return run(ctx, chartHopWorkflow(bumps)) }, nil)
}
