// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/rsl"
)

// fixtureMatrix mirrors the catalog: Cilium ranges from Cilium's docs and the
// one chart minimum 1.36 has.
func fixtureMatrix() Matrix {
	return Matrix{
		Kubernetes: []KubernetesRung{
			{Version: "1.33.13"},
			{Version: "1.34.13"},
			{Version: "1.35.10"},
			{Version: "1.36.5", Charts: map[string]string{"metallb": "0.16.1"}},
		},
		DefaultKubernetes: "1.36.5",
		Cilium: []CiliumRung{
			{Version: "1.18.16", MinKubernetes: "1.30", MaxKubernetes: "1.33"},
			{Version: "1.19.10", MinKubernetes: "1.31", MaxKubernetes: "1.34"},
			{Version: "1.20.2", MinKubernetes: "1.33", MaxKubernetes: "1.36"},
		},
		Charts: map[string]ChartRung{
			"alloy":            {Default: "1.13.1", Versions: []string{"1.4.0", "1.13.1"}},
			"external-secrets": {Default: "2.13.0", Versions: []string{"2.13.0"}},
			"metallb":          {Default: "0.16.1", Versions: []string{"0.15.2", "0.16.1"}},
			"metrics-server":   {Default: "3.14.0", Versions: []string{"3.13.0", "3.14.0"}},
			"node-exporter":    {Default: "4.59.0", Versions: []string{"4.59.0"}},
		},
	}
}

type shapeOpt func(*rsl.ClusterShape)

func clusterShape(kube, cilium string, opts ...shapeOpt) rsl.ClusterShape {
	s := rsl.ClusterShape{
		Observed:   true,
		Kubernetes: rsl.KubernetesShape{ControlPlane: kube, Kubelet: kube, NodeCount: 1},
		Cilium:     rsl.CiliumShape{Chart: cilium, Agent: cilium},
		Charts:     map[string]rsl.ChartShape{},
	}
	for _, o := range opts {
		o(&s)
	}
	return s
}

func withChart(name, live string) shapeOpt {
	return func(s *rsl.ClusterShape) {
		s.Charts[name] = rsl.ChartShape{Live: live, ChartName: name, Identity: rsl.IdentityCurrent}
	}
}

func withLegacy(name, live string) shapeOpt {
	return func(s *rsl.ClusterShape) {
		s.Charts[name] = rsl.ChartShape{Live: live, ChartName: name, Identity: rsl.IdentityLegacy, Reason: "legacy " + name}
	}
}

// fleetCharts are the charts a cluster built before the 1.36 refresh runs.
func fleetCharts() []shapeOpt {
	return []shapeOpt{withChart("metallb", "0.15.2"), withChart("metrics-server", "3.13.0"), withChart("alloy", "1.4.0")}
}

func kubeHop(step HopStep, from, to string) Hop {
	return Hop{Kind: KindKubernetes, Step: step, Component: "kubernetes", From: from, To: to}
}

func ciliumHop(step HopStep, from, to string) Hop {
	return Hop{Kind: KindCilium, Step: step, Component: "cilium", From: from, To: to}
}

func chartsHop(step HopStep, bumps ...ChartBump) Hop {
	return Hop{Kind: KindCharts, Step: step, Charts: bumps}
}

func bump(chart, from, to string) ChartBump { return ChartBump{Chart: chart, From: from, To: to} }

func resolve(t *testing.T, shape rsl.ClusterShape, opts Options) Plan {
	t.Helper()
	plan, err := Resolve(shape, fixtureMatrix(), opts)
	require.NoError(t, err)
	require.Empty(t, plan.Blockers)
	return plan
}

func TestResolve_FullLadder(t *testing.T) {
	plan := resolve(t, clusterShape("1.33.4", "1.18.1", fleetCharts()...), Options{})

	assert.Equal(t, Target{Kubernetes: "1.36.5", Cilium: "1.20.2"}, plan.Target)
	assert.Equal(t, []Hop{
		ciliumHop(StepPatch, "1.18.1", "1.18.16"),
		ciliumHop(StepMinor, "1.18.16", "1.19.10"),
		kubeHop(StepPatch, "1.33.4", "1.33.13"),
		kubeHop(StepMinor, "1.33.13", "1.34.13"),
		ciliumHop(StepMinor, "1.19.10", "1.20.2"), // 1.19 stops at Kubernetes 1.34
		kubeHop(StepMinor, "1.34.13", "1.35.10"),
		chartsHop(StepCoupled, bump("metallb", "0.15.2", "0.16.1")),
		kubeHop(StepMinor, "1.35.10", "1.36.5"),
		chartsHop(StepRefresh, bump("alloy", "1.4.0", "1.13.1"), bump("metrics-server", "3.13.0", "3.14.0")),
	}, plan.Hops)
}

func TestResolve_AlreadyCurrent(t *testing.T) {
	plan := resolve(t, clusterShape("1.36.5", "1.20.2", withChart("metallb", "0.16.1"), withChart("alloy", "1.13.1")), Options{})

	assert.Empty(t, plan.Hops)
	assert.Equal(t, Target{Kubernetes: "1.36.5", Cilium: "1.20.2"}, plan.Target)
}

// Cilium 1.19 stops at Kubernetes 1.34, so 1.35 still needs Cilium 1.20. A
// lower target gets no coupled bumps for minors past it and no refresh.
func TestResolve_ToMinor(t *testing.T) {
	want := []Hop{
		ciliumHop(StepPatch, "1.18.1", "1.18.16"),
		ciliumHop(StepMinor, "1.18.16", "1.19.10"),
		kubeHop(StepPatch, "1.33.4", "1.33.13"),
		kubeHop(StepMinor, "1.33.13", "1.34.13"),
		ciliumHop(StepMinor, "1.19.10", "1.20.2"),
		kubeHop(StepMinor, "1.34.13", "1.35.10"),
	}
	for _, to := range []string{"1.35", "1.35.10"} {
		t.Run(to, func(t *testing.T) {
			plan := resolve(t, clusterShape("1.33.4", "1.18.1", fleetCharts()...), Options{To: to})
			assert.Equal(t, Target{Kubernetes: "1.35.10", Cilium: "1.20.2"}, plan.Target)
			assert.Equal(t, want, plan.Hops)
		})
	}
}

// Cilium 1.19 runs on 1.34, so a 1.34 target leaves Cilium on 1.19.
func TestResolve_CiliumTargetFollowsKubernetes(t *testing.T) {
	plan := resolve(t, clusterShape("1.33.13", "1.18.16"), Options{To: "1.34"})

	assert.Equal(t, Target{Kubernetes: "1.34.13", Cilium: "1.19.10"}, plan.Target)
	assert.Equal(t, []Hop{
		ciliumHop(StepMinor, "1.18.16", "1.19.10"),
		kubeHop(StepMinor, "1.33.13", "1.34.13"),
	}, plan.Hops)
}

func TestResolve_LatestPatchesNeedNoPatchHops(t *testing.T) {
	plan := resolve(t, clusterShape("1.33.13", "1.18.16"), Options{})

	assert.Equal(t, []Hop{
		ciliumHop(StepMinor, "1.18.16", "1.19.10"),
		kubeHop(StepMinor, "1.33.13", "1.34.13"),
		ciliumHop(StepMinor, "1.19.10", "1.20.2"),
		kubeHop(StepMinor, "1.34.13", "1.35.10"),
		kubeHop(StepMinor, "1.35.10", "1.36.5"),
	}, plan.Hops)
}

func TestResolve_StaleTargetPatch(t *testing.T) {
	plan := resolve(t, clusterShape("1.36.2", "1.20.1"), Options{})

	assert.Equal(t, []Hop{
		ciliumHop(StepPatch, "1.20.1", "1.20.2"),
		kubeHop(StepPatch, "1.36.2", "1.36.5"),
	}, plan.Hops)
}

func TestResolve_StructuralOnly(t *testing.T) {
	plan := resolve(t, clusterShape("1.36.5", "1.20.2",
		withLegacy("external-secrets", "1.0.0"), withLegacy("node-exporter", "4.5.19"), withChart("metallb", "0.16.1")), Options{})

	assert.Equal(t, []Hop{
		{Kind: KindStructural, Step: StepReplace, Component: "node-exporter", From: "4.5.19", To: "4.59.0"},
		{Kind: KindStructural, Step: StepMigrate, Component: "external-secrets", From: "1.0.0", To: "2.13.0"},
	}, plan.Hops)
}

// Structural hops run before the ladder, and the charts they install are not
// refreshed again.
func TestResolve_StructuralBeforeLadder(t *testing.T) {
	plan := resolve(t, clusterShape("1.35.10", "1.20.2", withLegacy("node-exporter", "4.5.19")), Options{})

	assert.Equal(t, []Hop{
		{Kind: KindStructural, Step: StepReplace, Component: "node-exporter", From: "4.5.19", To: "4.59.0"},
		kubeHop(StepMinor, "1.35.10", "1.36.5"),
	}, plan.Hops)
}

func TestResolve_CoupledBumpForTheCurrentMinor(t *testing.T) {
	plan := resolve(t, clusterShape("1.36.5", "1.20.2", withChart("metallb", "0.15.2")), Options{})

	assert.Equal(t, []Hop{chartsHop(StepCoupled, bump("metallb", "0.15.2", "0.16.1"))}, plan.Hops)
}

func TestResolve_MissingChartIsNotBumped(t *testing.T) {
	shape := clusterShape("1.35.10", "1.20.2")
	shape.Charts["metallb"] = rsl.ChartShape{Missing: true}

	plan := resolve(t, shape, Options{})
	assert.Equal(t, []Hop{kubeHop(StepMinor, "1.35.10", "1.36.5")}, plan.Hops)
}

func TestResolve_ResumesAHalfDoneKubernetesHop(t *testing.T) {
	shape := clusterShape("1.34.13", "1.20.2")
	shape.Kubernetes.Kubelet = "1.33.13"

	plan := resolve(t, shape, Options{To: "1.35"})
	assert.Equal(t, []Hop{
		kubeHop(StepResume, "1.33.13", "1.34.13"),
		kubeHop(StepMinor, "1.34.13", "1.35.10"),
	}, plan.Hops)
}

func TestResolve_NoDowngrade(t *testing.T) {
	plan := resolve(t, clusterShape("1.36.6", "1.20.4", withChart("metallb", "0.16.2")), Options{})

	assert.Empty(t, plan.Hops)
	assert.Equal(t, Target{Kubernetes: "1.36.5", Cilium: "1.20.4"}, plan.Target)
}

func TestResolve_TargetErrors(t *testing.T) {
	tests := []struct {
		installed, to, wantErr string
	}{
		{"1.33.4", "1.35.2", "Kubernetes 1.35.2 is not an upgrade target; the 1.35 target is 1.35.10"},
		{"1.33.4", "1.32", "Kubernetes 1.32 is not a minor this binary can upgrade to"},
		{"1.33.4", "1.37", "Kubernetes 1.37 is not a minor this binary can upgrade to"},
		{"1.33.4", "latest", `target "latest" is not a minor such as 1.35 or a version such as 1.35.10`},
		{"1.33.4", "1.35.10-rc.1", `target "1.35.10-rc.1" is not a minor`},
		{"1.35.10", "1.34", "target Kubernetes 1.34.13 is below the installed 1.35.10"},
	}
	for _, tt := range tests {
		t.Run(tt.to, func(t *testing.T) {
			_, err := Resolve(clusterShape(tt.installed, "1.20.2"), fixtureMatrix(), Options{To: tt.to})
			require.Error(t, err)
			assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), err.Error())
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestResolve_TargetPastTheDefault(t *testing.T) {
	m := fixtureMatrix()
	m.DefaultKubernetes = "1.35.10"
	_, err := Resolve(clusterShape("1.33.13", "1.20.2"), m, Options{To: "1.36"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Kubernetes 1.36.5 is past the default 1.35.10")
}

func TestResolve_Blockers(t *testing.T) {
	tests := []struct {
		name   string
		shape  func() rsl.ClusterShape
		code   BlockerCode
		detail string
	}{
		{"not observed", func() rsl.ClusterShape {
			s := clusterShape("1.36.5", "1.20.2")
			s.Observed = false
			return s
		}, BlockerNotObserved, "could not be fully read"},
		{"more than one node", func() rsl.ClusterShape {
			s := clusterShape("1.36.5", "1.20.2")
			s.Kubernetes.NodeCount, s.Kubernetes.UnexpectedNodes = 3, true
			return s
		}, BlockerMultipleNodes, "the cluster has 3 nodes"},
		{"API server version unknown", func() rsl.ClusterShape { return clusterShape("", "1.20.2") },
			BlockerKubernetesUnknown, "the API server version is unknown"},
		{"API server version unreadable", func() rsl.ClusterShape {
			s := clusterShape("1.36.5-rc.1", "1.20.2")
			s.Kubernetes.Kubelet = "1.36.5"
			return s
		}, BlockerKubernetesUnknown, `the API server version "1.36.5-rc.1" is not a release version`},
		{"kubelet version unreadable", func() rsl.ClusterShape {
			s := clusterShape("1.36.5", "1.20.2")
			s.Kubernetes.Kubelet = "v1.36"
			return s
		}, BlockerKubernetesUnknown, `the kubelet version "v1.36"`},
		{"Kubernetes below the ladder", func() rsl.ClusterShape { return clusterShape("1.32.9", "1.18.16") },
			BlockerKubernetesNotLadder, "no upgrade path from minor 1.32"},
		{"Kubernetes above the ladder", func() rsl.ClusterShape { return clusterShape("1.37.1", "1.20.2") },
			BlockerKubernetesNotLadder, "no upgrade path from minor 1.37"},
		{"no Cilium release", func() rsl.ClusterShape { return clusterShape("1.36.5", "") },
			BlockerCiliumUnknown, "no Cilium release was found"},
		{"Cilium chart and agent differ", func() rsl.ClusterShape {
			s := clusterShape("1.36.5", "1.20.2")
			s.Cilium.Agent, s.Cilium.Mismatch = "1.19.10", true
			return s
		}, BlockerCiliumMismatch, "the Cilium chart is 1.20.2 but the agent runs 1.19.10"},
		{"Cilium below the ladder", func() rsl.ClusterShape { return clusterShape("1.33.4", "1.17.5") },
			BlockerCiliumNotLadder, "no upgrade path from minor 1.17"},
		{"unsupported pairing", func() rsl.ClusterShape { return clusterShape("1.34.13", "1.18.16") },
			BlockerUnsupportedPairing, "Cilium 1.18.16 does not support Kubernetes 1.34 (it supports 1.30 to 1.33)"},
		{"chart identity unknown", func() rsl.ClusterShape {
			s := clusterShape("1.36.5", "1.20.2")
			s.Charts["alloy"] = rsl.ChartShape{Live: "1.13.1", Identity: rsl.IdentityUnknown, Reason: "the release has no chart metadata"}
			return s
		}, BlockerChartUnknown, "the release has no chart metadata"},
		{"chart version unreadable", func() rsl.ClusterShape { return clusterShape("1.36.5", "1.20.2", withChart("alloy", "latest")) },
			BlockerChartUnknown, `chart version "latest"`},
		{"legacy chart with no migration", func() rsl.ClusterShape { return clusterShape("1.36.5", "1.20.2", withLegacy("alloy", "0.9.0")) },
			BlockerNoMigration, "legacy alloy; no migration exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := Resolve(tt.shape(), fixtureMatrix(), Options{})
			require.NoError(t, err)
			assert.Empty(t, plan.Hops)
			assert.Equal(t, "1.36.5", plan.Target.Kubernetes)
			require.Len(t, plan.Blockers, 1, "%+v", plan.Blockers)
			assert.Equal(t, tt.code, plan.Blockers[0].Code)
			assert.Contains(t, plan.Blockers[0].Detail, tt.detail)
		})
	}
}

func TestResolve_ListsEveryBlocker(t *testing.T) {
	s := clusterShape("1.32.9", "")
	s.Observed = false
	s.Charts["alloy"] = rsl.ChartShape{Identity: rsl.IdentityUnknown, Reason: "no metadata"}

	plan, err := Resolve(s, fixtureMatrix(), Options{})
	require.NoError(t, err)
	var codes []BlockerCode
	for _, b := range plan.Blockers {
		codes = append(codes, b.Code)
	}
	assert.Equal(t, []BlockerCode{BlockerNotObserved, BlockerKubernetesNotLadder, BlockerCiliumUnknown, BlockerChartUnknown}, codes)
}

func TestResolve_InvalidMatrix(t *testing.T) {
	_, err := Resolve(clusterShape("1.36.5", "1.20.2"), Matrix{}, Options{})
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalArgument), err.Error())
	assert.Contains(t, err.Error(), "invalid upgrade matrix")
}

func TestResolve_IsDeterministic(t *testing.T) {
	shape := clusterShape("1.33.4", "1.18.1", append(fleetCharts(), withLegacy("node-exporter", "4.5.19"), withLegacy("external-secrets", "1.0.0"))...)
	first := resolve(t, shape, Options{})
	for range 20 {
		assert.Equal(t, first, resolve(t, shape, Options{}))
	}
}

// Walking any plan from any plannable starting pair never skips a Kubernetes
// or Cilium minor, never leaves an unsupported pair, and ends on the target.
func TestResolve_EveryWalkStaysSupported(t *testing.T) {
	m := fixtureMatrix()
	c, err := m.compile()
	require.NoError(t, err)

	var starts []string
	for _, r := range c.kube {
		starts = append(starts, version{r.v.major, r.v.minor, 0}.String(), r.v.String())
	}
	var ciliums []string
	for _, r := range c.cilium {
		ciliums = append(ciliums, version{r.v.major, r.v.minor, 0}.String(), r.v.String())
	}
	targets := []string{"", "1.34", "1.35", "1.36"}

	walked := 0
	for _, k := range starts {
		for _, cil := range ciliums {
			for _, to := range targets {
				plan, err := Resolve(clusterShape(k, cil), m, Options{To: to})
				if err != nil || len(plan.Blockers) > 0 {
					continue // unsupported start, or a target below it
				}
				walked++
				kube, _ := parseVersion(k)
				cv, _ := parseVersion(cil)
				for _, h := range plan.Hops {
					from, _ := parseVersion(h.From)
					to, _ := parseVersion(h.To)
					switch h.Kind {
					case KindKubernetes:
						require.Equal(t, kube, from, "%s %s: hop %+v", k, cil, h)
						require.True(t, to.minor-from.minor <= 1, "%s %s skips a minor: %+v", k, cil, h)
						kube = to
					case KindCilium:
						require.Equal(t, cv, from, "%s %s: hop %+v", k, cil, h)
						require.True(t, to.minor-from.minor <= 1, "%s %s skips a minor: %+v", k, cil, h)
						cv = to
					}
					r, ok := c.ciliumRung(cv.minorOf())
					require.True(t, ok)
					require.True(t, r.supports(kube.minorOf()), "%s %s: Cilium %s on Kubernetes %s after %+v", k, cil, cv, kube, h)
				}
				target, _ := parseVersion(plan.Target.Kubernetes)
				if kube.less(target) {
					t.Fatalf("%s %s to %q ends on %s, not %s", k, cil, to, kube, target)
				}
				assert.Equal(t, plan.Target.Cilium, cv.String())
			}
		}
	}
	assert.Greater(t, walked, 20)
}
