// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// Seams so tests run no workflow.
var (
	nodeExporterReplaceWorkflow    = workflows.NodeExporterReplaceWorkflow
	externalSecretsMigrateWorkflow = workflows.ExternalSecretsMigrateWorkflow
)

// structuralWorkflow returns the workflow for a structural hop, or false when
// the resolver has no such hop.
func structuralWorkflow(hop Hop) (func() *automa.WorkflowBuilder, bool) {
	switch {
	case hop.Step == StepReplace && hop.Component == software.NodeExporterComponentName:
		return nodeExporterReplaceWorkflow, true
	case hop.Step == StepMigrate && hop.Component == software.ExternalSecretsComponentName:
		return func() *automa.WorkflowBuilder { return externalSecretsMigrateWorkflow(hop.To) }, true
	}
	return nil, false
}

// StructuralHopChecks checks that hop can run on the cluster in shape. It reads
// nothing and changes nothing; the live state is checked by the hop's first
// step.
func StructuralHopChecks(shape rsl.ClusterShape, m Matrix, hop Hop) (Checks, error) {
	if _, ok := structuralWorkflow(hop); hop.Kind != KindStructural || !ok {
		return nil, errx.Decorate(errorx.IllegalArgument.New("%s %s %s is not a structural hop this binary runs",
			hop.Kind, hop.Step, hop.Component), reasons.Internal)
	}
	if hop.From == "" {
		return nil, errx.Decorate(errorx.IllegalArgument.New("hop start: the legacy chart version must be set"), reasons.Internal)
	}
	if _, err := parseVersion(hop.To); err != nil {
		return nil, errx.Decorate(errorx.IllegalArgument.Wrap(err, "hop target"), reasons.Internal)
	}
	c, err := m.compile()
	if err != nil {
		return nil, errx.Decorate(errorx.IllegalFormat.Wrap(err, "invalid upgrade matrix"), reasons.Internal)
	}

	var cs Checks
	cs.add("the cluster was read", shape.Observed, "the cluster could not be fully read")
	cs.add("one node", !shape.Kubernetes.UnexpectedNodes,
		fmt.Sprintf("the cluster has %d nodes; upgrades support one", shape.Kubernetes.NodeCount))

	// A node-exporter swap that stopped after the uninstall leaves no release,
	// and can run again to finish.
	ch := shape.Charts[hop.Component]
	atStart := ch.Identity == rsl.IdentityLegacy && ch.Live == hop.From
	if hop.Step == StepReplace && ch.Missing {
		atStart = true
	}
	cs.add("the chart is where the hop starts", atStart,
		fmt.Sprintf("%s runs %q (%s), the hop starts at the legacy chart %s", hop.Component, ch.Live, identityOf(ch), hop.From))

	r, ok := c.charts[hop.Component]
	cs.add("the target is the catalog default", ok && r.def.String() == hop.To,
		fmt.Sprintf("%s %s is not the catalog default", hop.Component, hop.To))
	return cs, nil
}

func identityOf(ch rsl.ChartShape) string {
	if ch.Missing {
		return "missing"
	}
	return string(ch.Identity)
}

// ApplyStructuralHop runs one structural hop: the node-exporter swap or the
// External Secrets migration. The caller holds the system and cluster locks
// in locks for the whole run. It reads the cluster, does nothing when the hop
// is already applied, and refuses when a check fails. Otherwise one workflow
// runs the hop, waits for the cluster to settle and records it, and one entry
// goes to the action history.
func ApplyStructuralHop(ctx context.Context, src LiveSources, locks *state.HeldLocks, run WorkflowRunner, hop Hop) error {
	if run == nil {
		return errx.Decorate(errorx.IllegalArgument.New("a workflow runner must be set"), reasons.Internal)
	}
	if err := heldFor(src, locks, state.ComponentCluster); err != nil {
		return err
	}
	shape, m, err := readHopLive(ctx, src)
	if err != nil {
		return err
	}
	checks, err := StructuralHopChecks(shape, m, hop)
	if err != nil {
		return err
	}
	if ch := shape.Charts[hop.Component]; ch.Identity == rsl.IdentityCurrent && ch.Live == hop.To {
		logx.As().Info().Str("chart", hop.Component).Str("version", hop.To).Msg("The chart already runs the hop's target; nothing to do")
		return nil
	}
	if err := checks.Err(); err != nil {
		return err
	}

	workflow, _ := structuralWorkflow(hop)
	inputs := map[string]string{inputComponent: hop.Component, inputStep: string(hop.Step), inputFrom: hop.From, inputTo: hop.To}
	logx.As().Info().Str("component", hop.Component).Str("step", string(hop.Step)).Str("from", hop.From).Str("to", hop.To).
		Msg("Applying the structural hop")
	return runHop(ctx, src, run, inputs, workflow(), hopRecord(src, []state.ComponentID{state.ComponentCluster}, nil), nil)
}
