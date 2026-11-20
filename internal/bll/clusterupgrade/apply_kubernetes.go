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

var kubernetesHopWorkflow = workflows.KubernetesHopWorkflow

// KubernetesHopChecks checks that hop can run on the cluster in shape. It reads
// nothing and changes nothing. The live health of the node and the control
// plane is checked by the first step of the hop.
func KubernetesHopChecks(shape rsl.ClusterShape, m Matrix, hop Hop) (Checks, error) {
	if hop.Kind != KindKubernetes || (hop.Step != StepPatch && hop.Step != StepMinor && hop.Step != StepResume) {
		return nil, errx.Decorate(errorx.IllegalArgument.New("%s %s is not a Kubernetes patch, minor or resume hop", hop.Kind, hop.Step),
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

	k := shape.Kubernetes
	var cs Checks
	cs.add("the cluster was read", shape.Observed, "the cluster could not be fully read")
	cs.add("one node", !k.UnexpectedNodes, fmt.Sprintf("the cluster has %d nodes; upgrades support one", k.NodeCount))

	// A hop that stopped after the control plane moved can run again, so the
	// control plane may already be at the target.
	var atStart bool
	switch hop.Step {
	case StepResume:
		atStart = k.ControlPlane == hop.To && (k.Kubelet == hop.From || k.Kubelet == hop.To)
	default:
		atStart = (k.ControlPlane == hop.From && k.Kubelet == hop.From) ||
			(k.ControlPlane == hop.To && (k.Kubelet == hop.From || k.Kubelet == hop.To))
	}
	cs.add("Kubernetes is where the hop starts", atStart,
		fmt.Sprintf("the API server runs %q and the kubelet %q; the %s hop goes from %s to %s", k.ControlPlane, k.Kubelet, hop.Step, hop.From, hop.To))

	rung, shipped := c.kubeRung(to.minorOf())
	shipped = shipped && rung.v == to
	cs.add("the target is a catalog version", shipped, fmt.Sprintf("Kubernetes %s is not a version this binary upgrades to", to))

	var oneStep bool
	switch hop.Step {
	case StepPatch:
		oneStep = from.less(to) && from.minorOf() == to.minorOf()
	case StepMinor:
		oneStep = from.minorOf().next() == to.minorOf()
	case StepResume:
		oneStep = from.less(to) && resumable(from, to)
	}
	cs.add("one step at a time", oneStep, fmt.Sprintf("%s -> %s is not a %s hop", from, to, hop.Step))

	ciliumOK := false
	if v, err := parseVersion(shape.Cilium.Chart); err == nil {
		if r, ok := c.ciliumRung(v.minorOf()); ok {
			ciliumOK = r.supports(to.minorOf())
		}
	}
	cs.add("the running Cilium supports the target", ciliumOK,
		fmt.Sprintf("Cilium %q does not support Kubernetes %s; upgrade Cilium first", shape.Cilium.Chart, to.minorOf()))

	var missing []string
	if shipped {
		for _, name := range sortedKeys(rung.charts) {
			ch, ok := shape.Charts[name]
			if !ok || ch.Missing {
				continue
			}
			live, err := parseVersion(ch.Live)
			if ch.Identity != rsl.IdentityCurrent || err != nil || live.less(rung.charts[name]) {
				missing = append(missing, fmt.Sprintf("%s %s (runs %q)", name, rung.charts[name], ch.Live))
			}
		}
	}
	cs.add("the charts the target needs are installed", len(missing) == 0,
		fmt.Sprintf("Kubernetes %s needs %v first", to.minorOf(), missing))
	return cs, nil
}

// crioFor returns the cri-o version the cluster runs on Kubernetes kube: the
// latest catalog cri-o of the same minor. It also checks that the catalog has
// every Kubernetes host binary at kube.
func crioFor(cat *software.InfrastructureCatalog, kube string) (string, error) {
	v, err := parseVersion(kube)
	if err != nil {
		return "", errorx.IllegalArgument.Wrap(err, "Kubernetes version")
	}
	for _, name := range []string{software.KubeadmBinaryName, software.KubeletBinaryName, software.KubectlBinaryName} {
		art, err := cat.GetHostArtifact(name)
		if err != nil {
			return "", errorx.IllegalFormat.Wrap(err, "catalog")
		}
		if _, ok := art.Versions[software.Version(kube)]; !ok {
			return "", errorx.IllegalState.New("the catalog has no %s %s", name, kube)
		}
	}
	crio, err := cat.GetHostArtifact(software.CrioArtifactName)
	if err != nil {
		return "", errorx.IllegalFormat.Wrap(err, "catalog")
	}
	var best *version
	for s := range crio.Versions {
		c, err := parseVersion(string(s))
		if err != nil || c.minorOf() != v.minorOf() {
			continue
		}
		if best == nil || best.less(c) {
			best = &c
		}
	}
	if best == nil {
		return "", errorx.IllegalState.New("the catalog has no cri-o for Kubernetes %s", v.minorOf())
	}
	return best.String(), nil
}

// ApplyKubernetesHop moves the cluster by one Kubernetes hop. The caller holds
// the system and cluster locks in locks for the whole run. It reads the
// cluster, does nothing when the hop is already applied, and refuses when a
// check fails. Otherwise one workflow runs the hop, waits for the cluster to
// settle and records the host binaries and the cluster, and one entry goes to
// the action history. A failed hop still records the host binaries it may
// have replaced.
func ApplyKubernetesHop(ctx context.Context, src LiveSources, locks *state.HeldLocks, run WorkflowRunner, hop Hop) error {
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
	checks, err := KubernetesHopChecks(shape, m, hop)
	if err != nil {
		return err
	}
	cat, err := src.Catalog()
	if err != nil {
		return errx.Decorate(errorx.IllegalState.Wrap(err, "failed to load the catalog"), reasons.Internal)
	}
	crio, crioErr := crioFor(cat, hop.To)
	k := shape.Kubernetes
	if crioErr == nil && k.ControlPlane == hop.To && k.Kubelet == hop.To && k.ContainerRuntime == "cri-o://"+crio {
		logx.As().Info().Str("kubernetes", hop.To).Str("cri-o", crio).Msg("The cluster already runs the hop's target; nothing to do")
		return nil
	}
	if err := checks.Err(); err != nil {
		return err
	}
	if crioErr != nil {
		return errx.Decorate(crioErr, reasons.Internal)
	}

	inputs := map[string]string{inputComponent: componentKubernetes, inputStep: string(hop.Step),
		inputFrom: hop.From, inputTo: hop.To, inputCrio: crio}
	logx.As().Info().Str("step", string(hop.Step)).Str("from", hop.From).Str("to", hop.To).Str("cri-o", crio).
		Msg("Applying the Kubernetes hop")
	return runHop(ctx, src, run, inputs, kubernetesHopWorkflow(hop.From, hop.To, crio),
		hopRecord(src, []state.ComponentID{state.ComponentSystem, state.ComponentCluster}, recordKubeBinaries),
		hopRecord(src, []state.ComponentID{state.ComponentSystem}, recordKubeBinaries))
}

// Seam so tests need no binaries on disk.
var identifyHostVersions = software.IdentifyInstalledVersions

// recordKubeBinaries records the version each Kubernetes host artifact is
// found at by hashing its binaries. One that matches no single catalog version
// keeps its record.
func recordKubeBinaries(st *state.State) {
	if st.MachineState.Software == nil {
		st.MachineState.Software = map[string]state.SoftwareState{}
	}
	for _, a := range steps.KubernetesHostArtifacts() {
		found, err := identifyHostVersions(a.Name)
		if err != nil || len(found) != 1 {
			logx.As().Warn().Err(err).Str("software", a.Name).Strs("matchingVersions", found).
				Msg("Could not identify the installed version; keeping its record")
			continue
		}
		cur := st.MachineState.Software[a.RecordKey]
		cur.Name, cur.Version, cur.Installed = a.Name, found[0], true
		st.MachineState.Software[a.RecordKey] = cur
	}
}
