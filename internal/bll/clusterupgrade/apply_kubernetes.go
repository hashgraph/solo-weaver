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
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

var kubernetesHopWorkflow = workflows.KubernetesHopWorkflow

// KubernetesHopChecks checks that hop can run on the cluster in shape. It reads
// nothing and changes nothing. The live health of the node and the control
// plane is checked by the first step of the hop.
func KubernetesHopChecks(shape rsl.ClusterShape, m Matrix, hop Hop) (Checks, error) {
	if hop.Kind != KindKubernetes || (hop.Step != StepPatch && hop.Step != StepMinor && hop.Step != StepResume) {
		return nil, errorx.IllegalArgument.New("%s %s is not a Kubernetes patch, minor or resume hop", hop.Kind, hop.Step)
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

// ApplyKubernetesHop moves the cluster by one Kubernetes hop. It reads the
// cluster, does nothing when the hop is already applied, refuses when a check
// fails, then runs the hop under the system and cluster locks, records the new
// host binary versions and the cluster, and leaves an entry in the action
// history.
func ApplyKubernetesHop(ctx context.Context, src LiveSources, run WorkflowRunner, hop Hop) error {
	if run == nil {
		return errorx.IllegalArgument.New("a workflow runner must be set")
	}
	shape, m, err := readLive(ctx, src)
	if err != nil {
		return err
	}
	checks, err := KubernetesHopChecks(shape, m, hop)
	if err != nil {
		return err
	}
	cat, err := src.Catalog()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to load the catalog")
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
		return crioErr
	}

	inputs := map[string]string{"component": "kubernetes", "step": string(hop.Step), "from": hop.From, "to": hop.To, "cri-o": crio}
	ran := false
	err = recordHop(ctx, src, inputs,
		[]state.ComponentID{state.ComponentSystem, state.ComponentCluster},
		func() error {
			ran = true
			return run(ctx, kubernetesHopWorkflow(hop.From, hop.To, crio))
		},
		recordKubeBinaries)
	if err != nil && ran {
		recordBinariesAfterFailure(ctx, src)
	}
	return err
}

// kubeHostArtifacts are the host artifacts a Kubernetes hop moves, by their key
// in the recorded software state.
var kubeHostArtifacts = []struct{ key, artifact string }{
	{software.KubeadmBinaryName, software.KubeadmBinaryName},
	{software.KubeletBinaryName, software.KubeletBinaryName},
	{software.KubectlBinaryName, software.KubectlBinaryName},
	{software.CrioBinaryName, software.CrioArtifactName},
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
	for _, a := range kubeHostArtifacts {
		found, err := identifyHostVersions(a.artifact)
		if err != nil || len(found) != 1 {
			logx.As().Warn().Err(err).Str("software", a.artifact).Strs("matchingVersions", found).
				Msg("Could not identify the installed version; keeping its record")
			continue
		}
		cur := st.MachineState.Software[a.key]
		cur.Name, cur.Version, cur.Installed = a.artifact, found[0], true
		st.MachineState.Software[a.key] = cur
	}
}

// recordBinariesAfterFailure records the host binaries a failed hop may have
// replaced. Other commands verify a binary against its recorded version, so a
// stale record would make them refuse it, uninstall included.
func recordBinariesAfterFailure(ctx context.Context, src LiveSources) {
	entry := state.NewEntry(ctx, models.Intent{Action: models.ActionUpgrade, Target: models.TargetCluster},
		map[string]string{"component": "kubernetes", "record": "host binaries after a failed hop"})
	err := state.RunOwning(src.State, entry, []state.ComponentID{state.ComponentSystem},
		func() error { return nil },
		func(st *state.State) error {
			recordKubeBinaries(st)
			return nil
		})
	if err != nil {
		logx.As().Warn().Err(err).Msg("Could not record the host binaries after the failed hop")
	}
}
