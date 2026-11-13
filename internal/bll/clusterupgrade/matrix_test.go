// SPDX-License-Identifier: Apache-2.0

package clusterupgrade

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/software"
)

func TestMatrixValidate(t *testing.T) {
	require.NoError(t, fixtureMatrix().Validate())

	tests := []struct {
		name    string
		mutate  func(m *Matrix)
		wantErr string
	}{
		{"no Kubernetes rungs", func(m *Matrix) { m.Kubernetes = nil }, "no Kubernetes rungs"},
		{"Kubernetes gap", func(m *Matrix) { m.Kubernetes = append(m.Kubernetes[:1], m.Kubernetes[2:]...) },
			"Kubernetes rung 1.35.10 does not follow 1.33.13"},
		{"Kubernetes out of order", func(m *Matrix) { m.Kubernetes[0], m.Kubernetes[1] = m.Kubernetes[1], m.Kubernetes[0] },
			"Kubernetes rung 1.33.13 does not follow 1.34.13"},
		{"Kubernetes rung not a version", func(m *Matrix) { m.Kubernetes[0].Version = "1.33" }, `"1.33" is not a release version`},
		{"default not a version", func(m *Matrix) { m.DefaultKubernetes = "" }, `"" is not a release version`},
		{"default not its minor's rung", func(m *Matrix) { m.DefaultKubernetes = "1.36.4" },
			"default Kubernetes 1.36.4 is not the rung of its minor"},
		{"minimum names an unknown chart", func(m *Matrix) { m.Kubernetes[3].Charts = map[string]string{"ghost": "1.0.0"} },
			"Kubernetes 1.36 needs chart ghost, which the matrix does not list"},
		{"minimum not a listed version", func(m *Matrix) { m.Kubernetes[3].Charts = map[string]string{"metallb": "0.16.0"} },
			"Kubernetes 1.36 needs metallb 0.16.0, which is not a listed version"},
		{"chart default not listed", func(m *Matrix) { m.Charts["alloy"] = ChartRung{Default: "1.14.0", Versions: []string{"1.13.1"}} },
			"chart alloy: default 1.14.0 is not a listed version"},
		{"chart version not a version", func(m *Matrix) { m.Charts["alloy"] = ChartRung{Default: "1.13.1", Versions: []string{"1.13.1", "x"}} },
			`chart alloy, cause: common.illegal_format: "x" is not a release version`},
		{"no Cilium rungs", func(m *Matrix) { m.Cilium = nil }, "no Cilium rungs"},
		{"Cilium gap", func(m *Matrix) { m.Cilium = []CiliumRung{m.Cilium[0], m.Cilium[2]} },
			"Cilium rung 1.20.2 does not follow 1.18.16"},
		{"Cilium range reversed", func(m *Matrix) { m.Cilium[1].MinKubernetes, m.Cilium[1].MaxKubernetes = "1.34", "1.31" },
			"Cilium 1.19.10: Kubernetes range 1.34 to 1.31 is reversed"},
		{"Cilium range not a minor", func(m *Matrix) { m.Cilium[1].MaxKubernetes = "1.34.0" }, `"1.34.0" is not a minor version`},
		{"no Cilium for a Kubernetes step", func(m *Matrix) { m.Cilium[2].MinKubernetes = "1.35" },
			"no Cilium rung supports both Kubernetes 1.34 and 1.35"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fixtureMatrix()
			tt.mutate(&m)
			err := m.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// The catalog's own matrix gives the full ladder in the corrected order.
func TestMatrixFromCatalog(t *testing.T) {
	cat, err := software.LoadInfrastructureCatalog()
	require.NoError(t, err)

	m, err := MatrixFromCatalog(cat)
	require.NoError(t, err)

	want := fixtureMatrix()
	assert.Equal(t, want.Kubernetes[:3], dropEmptyCharts(m.Kubernetes[:3]))
	assert.Equal(t, want.Kubernetes[3], m.Kubernetes[3])
	assert.Equal(t, want.DefaultKubernetes, m.DefaultKubernetes)
	assert.Equal(t, want.Cilium, m.Cilium)
	assert.NotContains(t, m.Charts, software.CiliumComponentName)
	assert.Equal(t, ChartRung{Default: "0.16.1", Versions: []string{"0.15.2", "0.16.1"}}, m.Charts["metallb"])

	plan, err := Resolve(clusterShape("1.33.4", "1.18.1", withChart("metallb", "0.15.2")), m, Options{})
	require.NoError(t, err)
	assert.Equal(t, []Hop{
		ciliumHop(StepPatch, "1.18.1", "1.18.16"),
		ciliumHop(StepMinor, "1.18.16", "1.19.10"),
		kubeHop(StepPatch, "1.33.4", "1.33.13"),
		kubeHop(StepMinor, "1.33.13", "1.34.13"),
		ciliumHop(StepMinor, "1.19.10", "1.20.2"),
		kubeHop(StepMinor, "1.34.13", "1.35.10"),
		coupledHop("1.36", bump("metallb", "0.15.2", "0.16.1")),
		kubeHop(StepMinor, "1.35.10", "1.36.5"),
	}, plan.Hops)
}

func TestMatrixFromCatalog_NilCatalog(t *testing.T) {
	_, err := MatrixFromCatalog(nil)
	require.Error(t, err)
}

func dropEmptyCharts(rungs []KubernetesRung) []KubernetesRung {
	out := make([]KubernetesRung, len(rungs))
	for i, r := range rungs {
		out[i] = r
		if len(r.Charts) == 0 {
			out[i].Charts = nil
		}
	}
	return out
}

func TestParseVersion(t *testing.T) {
	v, err := parseVersion("v1.34.13")
	require.NoError(t, err)
	assert.Equal(t, version{1, 34, 13}, v)

	for _, bad := range []string{"", "1.34", "1.34.13.1", "1.34.x", "1.34.13-rc.1", "1.-34.0", "1.034.0", "+1.34.0"} {
		_, err := parseVersion(bad)
		assert.Error(t, err, bad)
	}
}

func TestParseMinor(t *testing.T) {
	m, err := parseMinor("1.34")
	require.NoError(t, err)
	assert.Equal(t, minorVersion{1, 34}, m)

	for _, bad := range []string{"", "1", "1.34.0", "v1.34", "1.x"} {
		_, err := parseMinor(bad)
		assert.Error(t, err, bad)
	}
}
