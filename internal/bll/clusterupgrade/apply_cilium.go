// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"context"
	"fmt"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// Seams so tests run no workflow and need no binaries on disk.
var (
	hostBinaryAt      = steps.HostBinaryAt
	ciliumHopWorkflow = workflows.CiliumHopWorkflow
)

// CiliumHopChecks checks that hop can run on the cluster in shape. It reads
// nothing and changes nothing. The live health of Cilium is checked by the
// first step of the hop.
func CiliumHopChecks(shape rsl.ClusterShape, m Matrix, hop Hop) (Checks, error) {
	if hop.Kind != KindCilium || (hop.Step != StepPatch && hop.Step != StepMinor) {
		return nil, errx.Decorate(errorx.IllegalArgument.New("%s %s is not a Cilium patch or minor hop", hop.Kind, hop.Step),
			reasons.Internal)
	}
	from, err := parseVersion(hop.From)
	if err != nil {
		return nil, errx.Decorate(errorx.IllegalArgument.Wrap(err, "hop start"), reasons.Internal)
	}
	to, err := parseVersion(hop.To)
	if err != nil {
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
	cs.add("Cilium is where the hop starts", shape.Cilium.Chart == hop.From,
		fmt.Sprintf("the Cilium chart is %q, the hop starts at %s", shape.Cilium.Chart, hop.From))
	cs.add("the Cilium agent matches its chart", shape.Cilium.Agent != "" && !shape.Cilium.Mismatch,
		fmt.Sprintf("the agent runs %q and the chart is %q", shape.Cilium.Agent, shape.Cilium.Chart))

	rung, shipped := c.ciliumRung(to.minorOf())
	shipped = shipped && rung.v == to
	cs.add("the target is a catalog version", shipped, fmt.Sprintf("Cilium %s is not a version this binary ships", to))

	var oneStep bool
	switch hop.Step {
	case StepPatch:
		oneStep = from.less(to) && from.minorOf() == to.minorOf()
	case StepMinor:
		oneStep = from.minorOf().next() == to.minorOf()
	}
	cs.add("one step at a time", oneStep, fmt.Sprintf("%s -> %s is not a %s hop", from, to, hop.Step))

	kube, err := parseVersion(shape.Kubernetes.ControlPlane)
	cs.add("the target runs on this Kubernetes", err == nil && shipped && rung.supports(kube.minorOf()),
		fmt.Sprintf("Cilium %s does not support Kubernetes %q", to, shape.Kubernetes.ControlPlane))
	return cs, nil
}

// ApplyCiliumHop moves the running Cilium by one hop. The caller holds the
// system and cluster locks in locks for the whole run. It reads the cluster,
// does nothing when the hop is already applied, and refuses when a check
// fails. Otherwise one workflow puts the cilium CLI on the catalog default
// when it is not, runs the hop, waits for the cluster to settle and records
// it and the CLI, and one entry goes to the action history. A failed hop still
// records the CLI when it is at the catalog default.
func ApplyCiliumHop(ctx context.Context, src LiveSources, locks *state.HeldLocks, run WorkflowRunner, hop Hop) error {
	if run == nil {
		return errx.Decorate(errorx.IllegalArgument.New("a workflow runner must be set"), reasons.Internal)
	}
	if err := heldFor(src, locks, state.ComponentSystem, state.ComponentCluster); err != nil {
		return err
	}
	shape, m, err := readHopLive(ctx, src)
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
		return errx.Decorate(errorx.IllegalState.Wrap(err, "failed to load the catalog"), reasons.Internal)
	}
	cliArtifact, err := cat.GetHostArtifact(software.CiliumBinaryName)
	if err != nil {
		return errx.Decorate(errorx.IllegalFormat.Wrap(err, "catalog"), reasons.Internal)
	}
	cli := string(cliArtifact.Default)

	// The CLI is recorded even when this hop does not replace it: an earlier
	// run may have replaced it without recording it.
	inputs := map[string]string{inputComponent: componentCilium, inputFrom: hop.From, inputTo: hop.To, inputCiliumCLI: cli}
	written := []state.ComponentID{state.ComponentSystem, state.ComponentCluster}
	patch := func(st *state.State) { recordSoftware(st, software.CiliumBinaryName, cli) }
	logx.As().Info().Str("from", hop.From).Str("to", hop.To).Str("cli", cli).Msg("Applying the Cilium hop")
	return runHop(ctx, src, run, inputs, ciliumHopWorkflow(hop.From, hop.To, cli), hopRecord(src, written, patch),
		cliAfterFailure(src, cli))
}

// cliAfterFailure records the cilium CLI at cli when it is at cli after a
// failed hop, which may have replaced it. Other commands verify a binary
// against its recorded version, so a stale record would make them refuse it.
func cliAfterFailure(src LiveSources, cli string) recordFunc {
	write := hopRecord(src, []state.ComponentID{state.ComponentSystem},
		func(st *state.State) { recordSoftware(st, software.CiliumBinaryName, cli) })
	return func(ctx context.Context) ([]state.ChangedFile, error) {
		at, err := hostBinaryAt(software.CiliumBinaryName, cli)
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to read the installed cilium CLI")
		}
		if !at {
			return nil, nil
		}
		return write(ctx)
	}
}
