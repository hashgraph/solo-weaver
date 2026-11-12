// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"fmt"
	"maps"

	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/rsl"
)

const (
	componentKubernetes = "kubernetes"
	componentCilium     = "cilium"
)

// structuralSteps are the legacy charts the resolver can migrate, in the order
// their hops run. A legacy chart not listed here blocks the plan.
var structuralSteps = []struct {
	chart string
	step  HopStep
}{
	{"node-exporter", StepReplace},
	{"external-secrets", StepMigrate},
}

// Resolve returns the hops that take the cluster from shape to the target in
// m, bounded by opts. The order is: finish a half-done Kubernetes hop, then
// the structural migrations, then the version ladder, then one charts refresh
// when the target is the default. On the ladder Kubernetes moves one minor at
// a time, a minor hop lands on the next minor's latest patch, and Cilium moves
// first whenever it does not run on the next Kubernetes minor, so the running
// pair is always supported.
//
// A cluster that cannot be planned gets a plan with blockers and no hops. An
// error means m or opts is invalid.
func Resolve(shape rsl.ClusterShape, m Matrix, opts Options) (Plan, error) {
	c, err := m.compile()
	if err != nil {
		return Plan{}, errorx.IllegalArgument.Wrap(err, "invalid upgrade matrix")
	}
	target, err := c.target(opts.To)
	if err != nil {
		return Plan{}, err
	}

	in, blockers := readShape(shape, c)
	if len(blockers) > 0 {
		return Plan{Target: Target{Kubernetes: target.String()}, Blockers: blockers}, nil
	}
	if target.minorOf().compare(in.kube.minorOf()) < 0 {
		return Plan{}, errorx.IllegalArgument.New("target Kubernetes %s is below the installed %s", target, in.kube)
	}

	var plan Plan
	add := func(h Hop) { plan.Hops = append(plan.Hops, h) }
	kube, cil, charts := in.kube, in.cilium, maps.Clone(in.charts)

	if in.kubelet != nil && in.kubelet.less(kube) {
		add(Hop{Kind: KindKubernetes, Step: StepResume, Component: componentKubernetes, From: in.kubelet.String(), To: kube.String()})
	}

	for _, s := range structuralSteps {
		if from, ok := in.legacy[s.chart]; ok {
			def := c.charts[s.chart].def
			add(Hop{Kind: KindStructural, Step: s.step, Component: s.chart, From: from, To: def.String()})
			charts[s.chart] = def
		}
	}

	// A cluster can sit on a minor with charts older than that minor needs.
	if bumps := coupledBumps(c, kube.minorOf(), charts); len(bumps) > 0 {
		add(Hop{Kind: KindCharts, Step: StepCoupled, Charts: bumps})
	}

	if r, _ := c.ciliumRung(cil.minorOf()); cil.less(r.v) {
		add(Hop{Kind: KindCilium, Step: StepPatch, Component: componentCilium, From: cil.String(), To: r.v.String()})
		cil = r.v
	}

	for k := kube.minorOf(); k.compare(target.minorOf()) < 0; k = k.next() {
		for {
			r, _ := c.ciliumRung(cil.minorOf())
			if r.supports(k.next()) {
				break
			}
			next, ok := c.ciliumRung(cil.minorOf().next())
			if !ok || !next.supports(k) {
				return Plan{}, errorx.IllegalState.New("no Cilium step from %s runs on both Kubernetes %s and %s", cil, k, k.next())
			}
			add(Hop{Kind: KindCilium, Step: StepMinor, Component: componentCilium, From: cil.String(), To: next.v.String()})
			cil = next.v
		}
		if r, _ := c.kubeRung(k); kube.less(r.v) {
			add(Hop{Kind: KindKubernetes, Step: StepPatch, Component: componentKubernetes, From: kube.String(), To: r.v.String()})
			kube = r.v
		}
		if bumps := coupledBumps(c, k.next(), charts); len(bumps) > 0 {
			add(Hop{Kind: KindCharts, Step: StepCoupled, Charts: bumps})
		}
		next, _ := c.kubeRung(k.next())
		add(Hop{Kind: KindKubernetes, Step: StepMinor, Component: componentKubernetes, From: kube.String(), To: next.v.String()})
		kube = next.v
	}
	if kube.less(target) {
		add(Hop{Kind: KindKubernetes, Step: StepPatch, Component: componentKubernetes, From: kube.String(), To: target.String()})
	}

	if target == c.def {
		var bumps []ChartBump
		for _, name := range sortedKeys(charts) {
			if r, ok := c.charts[name]; ok && charts[name].less(r.def) {
				bumps = append(bumps, ChartBump{Chart: name, From: charts[name].String(), To: r.def.String()})
			}
		}
		if len(bumps) > 0 {
			add(Hop{Kind: KindCharts, Step: StepRefresh, Charts: bumps})
		}
	}

	plan.Target = Target{Kubernetes: target.String(), Cilium: cil.String()}
	return plan, nil
}

// coupledBumps raises the charts that minor k needs to their minimum, records
// the new versions in charts, and returns the bumps. A chart the cluster does
// not run is left alone.
func coupledBumps(c compiled, k minorVersion, charts map[string]version) []ChartBump {
	r, ok := c.kubeRung(k)
	if !ok {
		return nil
	}
	var out []ChartBump
	for _, name := range sortedKeys(r.charts) {
		cur, ok := charts[name]
		if !ok || !cur.less(r.charts[name]) {
			continue
		}
		out = append(out, ChartBump{Chart: name, From: cur.String(), To: r.charts[name].String()})
		charts[name] = r.charts[name]
	}
	return out
}

// target resolves opts.To against the ladder.
func (c compiled) target(to string) (version, error) {
	if to == "" {
		return c.def, nil
	}
	var want version
	m, err := parseMinor(to)
	exact := err != nil
	if exact {
		want, err = parseVersion(to)
		if err != nil {
			return version{}, errorx.IllegalArgument.New("target %q is not a minor such as 1.35 or a version such as 1.35.10", to)
		}
		m = want.minorOf()
	}
	r, ok := c.kubeRung(m)
	if !ok {
		return version{}, errorx.IllegalArgument.New("Kubernetes %s is not a minor this binary can upgrade to", m)
	}
	if exact && r.v != want {
		return version{}, errorx.IllegalArgument.New("Kubernetes %s is not an upgrade target; the %s target is %s", want, m, r.v)
	}
	if c.def.less(r.v) {
		return version{}, errorx.IllegalArgument.New("Kubernetes %s is past the default %s", r.v, c.def)
	}
	return r.v, nil
}

// installed is the part of the shape the resolver plans from.
type installed struct {
	kube    version
	kubelet *version // nil when the node did not report one
	cilium  version
	// charts holds every chart the cluster runs under its current identity.
	charts map[string]version
	// legacy holds the live version of each legacy chart with a migration.
	legacy map[string]string
}

// readShape parses shape and lists every reason it cannot be planned.
func readShape(shape rsl.ClusterShape, c compiled) (installed, []Blocker) {
	in := installed{charts: map[string]version{}, legacy: map[string]string{}}
	var out []Blocker
	block := func(code BlockerCode, component, format string, args ...any) {
		out = append(out, Blocker{Code: code, Component: component, Detail: fmt.Sprintf(format, args...)})
	}

	if !shape.Observed {
		block(BlockerNotObserved, "", "the cluster could not be fully read")
	}
	if shape.Kubernetes.UnexpectedNodes {
		block(BlockerMultipleNodes, componentKubernetes, "the cluster has %d nodes; upgrades support one", shape.Kubernetes.NodeCount)
	}

	kubeOK := false
	if shape.Kubernetes.ControlPlane == "" {
		block(BlockerKubernetesUnknown, componentKubernetes, "the API server version is unknown")
	} else if v, err := parseVersion(shape.Kubernetes.ControlPlane); err != nil {
		block(BlockerKubernetesUnknown, componentKubernetes, "the API server version %q is not a release version", shape.Kubernetes.ControlPlane)
	} else if _, ok := c.kubeRung(v.minorOf()); !ok {
		block(BlockerKubernetesNotLadder, componentKubernetes, "Kubernetes %s: this binary has no upgrade path from minor %s", v, v.minorOf())
	} else {
		in.kube, kubeOK = v, true
	}
	if s := shape.Kubernetes.Kubelet; s != "" {
		if v, err := parseVersion(s); err != nil {
			block(BlockerKubernetesUnknown, componentKubernetes, "the kubelet version %q is not a release version", s)
		} else {
			in.kubelet = &v
		}
	}

	switch ch := shape.Cilium; {
	case ch.Chart == "":
		block(BlockerCiliumUnknown, componentCilium, "no Cilium release was found")
	case ch.Mismatch:
		block(BlockerCiliumMismatch, componentCilium, "the Cilium chart is %s but the agent runs %s", ch.Chart, ch.Agent)
	default:
		v, err := parseVersion(ch.Chart)
		if err != nil {
			block(BlockerCiliumUnknown, componentCilium, "the Cilium chart version %q is not a release version", ch.Chart)
			break
		}
		r, ok := c.ciliumRung(v.minorOf())
		if !ok {
			block(BlockerCiliumNotLadder, componentCilium, "Cilium %s: this binary has no upgrade path from minor %s", v, v.minorOf())
			break
		}
		in.cilium = v
		if kubeOK && !r.supports(in.kube.minorOf()) {
			block(BlockerUnsupportedPairing, componentCilium, "Cilium %s does not support Kubernetes %s (it supports %s to %s)",
				v, in.kube.minorOf(), r.min, r.max)
		}
	}

	for _, name := range sortedKeys(shape.Charts) {
		ch := shape.Charts[name]
		if ch.Missing {
			continue
		}
		switch ch.Identity {
		case rsl.IdentityLegacy:
			if !hasMigration(name) || !hasChart(c, name) {
				block(BlockerNoMigration, name, "%s; no migration exists", ch.Reason)
				continue
			}
			in.legacy[name] = ch.Live
		case rsl.IdentityCurrent:
			v, err := parseVersion(ch.Live)
			if err != nil {
				block(BlockerChartUnknown, name, "chart version %q is not a release version", ch.Live)
				continue
			}
			in.charts[name] = v
		default:
			block(BlockerChartUnknown, name, "%s", ch.Reason)
		}
	}
	return in, out
}

func hasMigration(chart string) bool {
	for _, s := range structuralSteps {
		if s.chart == chart {
			return true
		}
	}
	return false
}

func hasChart(c compiled, chart string) bool {
	_, ok := c.charts[chart]
	return ok
}
