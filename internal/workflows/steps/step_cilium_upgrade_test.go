// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/golang/mock/gomock"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

func healthyCilium(version string) ciliumHealth {
	return ciliumHealth{
		AgentVersion: version, AgentsDesired: 1, AgentsUpdated: 1, AgentsAvailable: 1, AgentsRolledOut: true,
		OperatorDesired: 1, OperatorAvailable: 1, HostLegacyRouting: "true", LBAcceleration: "disabled",
	}
}

func TestCiliumHealthProblems(t *testing.T) {
	assert.Empty(t, healthyCilium("1.19.10").problems("1.19.10"))

	tests := []struct {
		name   string
		mutate func(*ciliumHealth)
		want   string
	}{
		{"agent on another version", func(h *ciliumHealth) { h.AgentVersion = "1.18.16" }, `the agent runs "1.18.16", not 1.19.10`},
		{"rollout not observed", func(h *ciliumHealth) { h.AgentsRolledOut = false }, "agents: 1 desired"},
		{"agent not updated", func(h *ciliumHealth) { h.AgentsUpdated = 0 }, "agents: 1 desired, 0 updated"},
		{"agent not available", func(h *ciliumHealth) { h.AgentsAvailable = 0 }, "0 available"},
		{"no agents scheduled", func(h *ciliumHealth) { h.AgentsDesired, h.AgentsUpdated, h.AgentsAvailable = 0, 0, 0 }, "agents: 0 desired"},
		{"operator down", func(h *ciliumHealth) { h.OperatorAvailable = 0 }, "operator: 0 of 1 available"},
		{"host legacy routing lost", func(h *ciliumHealth) { h.HostLegacyRouting = "false" }, `enable-host-legacy-routing is "false"`},
		{"acceleration on", func(h *ciliumHealth) { h.LBAcceleration = "best-effort" }, `bpf-lb-acceleration is "best-effort"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := healthyCilium("1.19.10")
			tt.mutate(&h)
			p := h.problems("1.19.10")
			require.Len(t, p, 1)
			assert.Contains(t, p[0], tt.want)
		})
	}
}

func TestCiliumHopUpgradeCommand(t *testing.T) {
	cmd := ciliumHopUpgradeCommand("1.19.10", "/charts/cilium-1.19.10/cilium", "/cfg/cilium-config.yaml")
	assert.Contains(t, cmd, `cilium upgrade --version "1.19.10" --chart-directory '/charts/cilium-1.19.10/cilium' --values '/cfg/cilium-config.yaml' --wait`)
}

// ciliumHopHarness stubs every outside effect of a Cilium hop.
type ciliumHopHarness struct {
	health   []ciliumHealth // returned in turn; the last one repeats
	charts   []string
	scripts  []string
	verified []string
	config   string
	helm     *helm.MockManager
}

func newCiliumHopHarness(t *testing.T, health ...ciliumHealth) *ciliumHopHarness {
	t.Helper()
	h := &ciliumHopHarness{health: health, helm: helm.NewMockManager(gomock.NewController(t))}
	origHealth, origFetch, origRender, origHelm, origRun, origVerify, origPoll :=
		readCiliumHealth, fetchCiliumChart, rerenderCiliumConfig, newCiliumHopHelm, runCiliumScript, verifyExecutables, ciliumPollInterval
	t.Cleanup(func() {
		readCiliumHealth, fetchCiliumChart, rerenderCiliumConfig, newCiliumHopHelm, runCiliumScript, verifyExecutables, ciliumPollInterval =
			origHealth, origFetch, origRender, origHelm, origRun, origVerify, origPoll
	})

	readCiliumHealth = func(context.Context, *kube.Client) (ciliumHealth, error) {
		next := h.health[0]
		if len(h.health) > 1 {
			h.health = h.health[1:]
		}
		return next, nil
	}
	fetchCiliumChart = func(_ context.Context, r ciliumRelease) (string, error) {
		h.charts = append(h.charts, r.Version)
		return "/charts/cilium-" + r.Version + "/cilium", nil
	}
	h.config = writeCiliumConfig(t, "k8sServiceHost: 10.0.0.5\nk8sServicePort: 6443\nhubble:\n  relay:\n    enabled: true\n")
	rerenderCiliumConfig = func() (string, error) { return h.config, nil }
	newCiliumHopHelm = func() (helm.Manager, error) { return h.helm, nil }
	runCiliumScript = func(scripts []string, _ string) (string, error) {
		h.scripts = append(h.scripts, scripts...)
		return "", nil
	}
	verifyExecutables = func(name string) error {
		h.verified = append(h.verified, name)
		return nil
	}
	ciliumPollInterval = time.Millisecond
	return h
}

func runSteps(t *testing.T, builders []automa.Builder) []*automa.Report {
	t.Helper()
	var reports []*automa.Report
	for _, b := range builders {
		stp, err := b.Build()
		require.NoError(t, err)
		rpt := stp.Execute(context.Background())
		reports = append(reports, rpt)
		if rpt.Status == automa.StatusFailed {
			break
		}
	}
	return reports
}

// The hop's own steps, without the bandwidth guard, which reads the cluster
// through its own client.
func hopSteps(from, to string) []automa.Builder {
	all := CiliumHopSteps(from, to, func() (*kube.Client, error) { return &kube.Client{}, nil })
	return all[:len(all)-1]
}

func TestCiliumHop_UpgradesThroughPreflight(t *testing.T) {
	h := newCiliumHopHarness(t,
		healthyCilium("1.18.16"), // check-cilium-healthy: not done
		healthyCilium("1.18.16"), // check-cilium-healthy: healthy at from
		healthyCilium("1.18.16"), // prepare: not done
		healthyCilium("1.19.10"), // wait-cilium-healthy
	)
	gomock.InOrder(
		h.helm.EXPECT().IsInstalled(ciliumPreflightRelease, "kube-system").Return(false, nil),
		h.helm.EXPECT().InstallChart(gomock.Any(), ciliumPreflightRelease, "/charts/cilium-1.19.10/cilium", "1.19.10", "kube-system", gomock.Any()).
			DoAndReturn(func(_ context.Context, _, _, _, _ string, o helm.InstallChartOptions) (*release.Release, error) {
				assert.Empty(t, o.ValueOpts.ValueFiles, "our values would put Hubble relay in the pre-flight release")
				assert.Equal(t, []string{"preflight.enabled=true", "agent=false", "operator.enabled=false",
					"k8sServiceHost=10.0.0.5", "k8sServicePort=6443"}, o.ValueOpts.Values)
				assert.True(t, o.Wait)
				return &release.Release{}, nil
			}),
		h.helm.EXPECT().IsInstalled(ciliumPreflightRelease, "kube-system").Return(true, nil),
		h.helm.EXPECT().UninstallChart(ciliumPreflightRelease, "kube-system").Return(nil),
	)

	reports := runSteps(t, hopSteps("1.18.16", "1.19.10"))

	require.Len(t, reports, 5)
	for _, r := range reports {
		require.NoError(t, r.Error)
		assert.Equal(t, automa.StatusSuccess, r.Status)
	}
	assert.Equal(t, []string{"1.19.10"}, h.charts)
	assert.Equal(t, []string{"cilium"}, h.verified)
	require.Len(t, h.scripts, 1)
	assert.Contains(t, h.scripts[0], `cilium upgrade --version "1.19.10" --chart-directory`)
}

func writeCiliumConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cilium-config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestCiliumPreflightValues_RejectsBadConfig(t *testing.T) {
	for name, content := range map[string]string{
		"no host":        "k8sServicePort: 6443\n",
		"blank host":     "k8sServiceHost: ' '\nk8sServicePort: 6443\n",
		"no port":        "k8sServiceHost: 10.0.0.5\n",
		"port too large": "k8sServiceHost: 10.0.0.5\nk8sServicePort: 70000\n",
		"negative port":  "k8sServiceHost: 10.0.0.5\nk8sServicePort: -1\n",
		"not yaml":       "k8sServiceHost: [\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ciliumPreflightValues(writeCiliumConfig(t, content))
			require.Error(t, err)
			assert.True(t, errorx.IsOfType(err, errorx.IllegalFormat), err.Error())
		})
	}
	t.Run("missing file", func(t *testing.T) {
		_, err := ciliumPreflightValues(filepath.Join(t.TempDir(), "absent.yaml"))
		require.Error(t, err)
		assert.True(t, errorx.IsOfType(err, errorx.IllegalState), err.Error())
	})
}

func TestCiliumHop_BadConfigStopsBeforePreflight(t *testing.T) {
	h := newCiliumHopHarness(t, healthyCilium("1.18.16"))
	h.config = writeCiliumConfig(t, "k8sServicePort: 6443\n")

	reports := runSteps(t, hopSteps("1.18.16", "1.19.10"))

	require.Len(t, reports, 3)
	assert.Equal(t, automa.StatusFailed, reports[2].Status)
	assert.Contains(t, reports[2].Error.Error(), "k8sServiceHost")
	assert.Empty(t, h.scripts) // the mock fails the test on any helm call
}

func TestCiliumHop_DoneHopSkipsEverything(t *testing.T) {
	h := newCiliumHopHarness(t, healthyCilium("1.19.10"))

	reports := runSteps(t, hopSteps("1.18.16", "1.19.10"))

	require.Len(t, reports, 5)
	for _, r := range reports[:4] {
		assert.Equal(t, automa.StatusSkipped, r.Status, r.Id)
	}
	assert.Equal(t, automa.StatusSuccess, reports[4].Status) // the health gate passes
	assert.Empty(t, h.charts)
	assert.Empty(t, h.scripts)
}

func TestCiliumHop_UnhealthyBeforeChangesNothing(t *testing.T) {
	sick := healthyCilium("1.18.16")
	sick.AgentsAvailable = 0
	h := newCiliumHopHarness(t, sick)

	reports := runSteps(t, hopSteps("1.18.16", "1.19.10"))

	require.Len(t, reports, 1)
	assert.Equal(t, automa.StatusFailed, reports[0].Status)
	assert.Contains(t, reports[0].Error.Error(), "Cilium 1.18.16 is not healthy before the upgrade")
	assert.Empty(t, h.charts)
	assert.Empty(t, h.scripts)
}

func TestCiliumHop_FailedPreflightIsRemovedAndCiliumUntouched(t *testing.T) {
	h := newCiliumHopHarness(t, healthyCilium("1.18.16"))
	gomock.InOrder(
		h.helm.EXPECT().IsInstalled(ciliumPreflightRelease, "kube-system").Return(true, nil), // left by an earlier run
		h.helm.EXPECT().UninstallChart(ciliumPreflightRelease, "kube-system").Return(nil),
		h.helm.EXPECT().InstallChart(gomock.Any(), ciliumPreflightRelease, gomock.Any(), "1.19.10", "kube-system", gomock.Any()).
			Return(nil, assert.AnError),
		h.helm.EXPECT().IsInstalled(ciliumPreflightRelease, "kube-system").Return(true, nil),
		h.helm.EXPECT().UninstallChart(ciliumPreflightRelease, "kube-system").Return(nil),
	)

	reports := runSteps(t, hopSteps("1.18.16", "1.19.10"))

	require.Len(t, reports, 3)
	assert.Equal(t, automa.StatusFailed, reports[2].Status)
	assert.Contains(t, reports[2].Error.Error(), "pre-flight check for 1.19.10 did not become ready")
	assert.Empty(t, h.scripts, "cilium upgrade must not run after a failed pre-flight")
}

func TestCiliumHop_HealthGateTimesOut(t *testing.T) {
	stuck := healthyCilium("1.18.16")
	h := newCiliumHopHarness(t, healthyCilium("1.18.16"), healthyCilium("1.18.16"), healthyCilium("1.18.16"), stuck)
	h.helm.EXPECT().IsInstalled(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()
	h.helm.EXPECT().InstallChart(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&release.Release{}, nil)
	orig := ciliumHealthTimeout
	ciliumHealthTimeout = 10 * time.Millisecond
	t.Cleanup(func() { ciliumHealthTimeout = orig })

	reports := runSteps(t, hopSteps("1.18.16", "1.19.10"))

	last := reports[len(reports)-1]
	assert.Equal(t, "wait-cilium-healthy", last.Id)
	assert.Equal(t, automa.StatusFailed, last.Status)
	assert.Contains(t, last.Error.Error(), `the agent runs "1.18.16", not 1.19.10`)
}

func TestUpgradeHostBinary(t *testing.T) {
	run := func(t *testing.T, archived bool, found [][]string, expect func(*software.MockSoftware)) (*automa.Report, []software.InstallerOption) {
		t.Helper()
		orig := identifyInstalledVersions
		t.Cleanup(func() { identifyInstalledVersions = orig })
		identifyInstalledVersions = func(string) ([]string, error) {
			next := found[0]
			if len(found) > 1 {
				found = found[1:]
			}
			return next, nil
		}
		mock := software.NewMockSoftware(gomock.NewController(t))
		if expect != nil {
			expect(mock)
		}
		var opts []software.InstallerOption
		stp, err := UpgradeHostBinary("cilium", "0.20.1", archived, func(o ...software.InstallerOption) (software.Software, error) {
			opts = o
			return mock, nil
		}).Build()
		require.NoError(t, err)
		return stp.Execute(context.Background()), opts
	}

	t.Run("already at the version", func(t *testing.T) {
		rpt, _ := run(t, true, [][]string{{"0.20.1"}}, nil)
		assert.Equal(t, automa.StatusSkipped, rpt.Status)
	})
	t.Run("unpacks an archived artifact before install", func(t *testing.T) {
		rpt, opts := run(t, true, [][]string{{"0.18.7"}, {"0.20.1"}}, func(m *software.MockSoftware) {
			gomock.InOrder(m.EXPECT().Download(), m.EXPECT().Extract(), m.EXPECT().Install(), m.EXPECT().Configure(), m.EXPECT().Cleanup())
		})
		require.NoError(t, rpt.Error)
		assert.Equal(t, automa.StatusSuccess, rpt.Status)
		assert.Len(t, opts, 1, "the installer is pinned to the version")
	})
	t.Run("installs a plain binary without unpacking", func(t *testing.T) {
		rpt, _ := run(t, false, [][]string{{"0.18.7"}, {"0.20.1"}}, func(m *software.MockSoftware) {
			gomock.InOrder(m.EXPECT().Download(), m.EXPECT().Install(), m.EXPECT().Configure(), m.EXPECT().Cleanup())
		})
		require.NoError(t, rpt.Error)
		assert.Equal(t, automa.StatusSuccess, rpt.Status)
	})
	t.Run("extract fails", func(t *testing.T) {
		rpt, _ := run(t, true, [][]string{{"0.18.7"}}, func(m *software.MockSoftware) {
			m.EXPECT().Download()
			m.EXPECT().Extract().Return(assert.AnError)
		})
		assert.Equal(t, automa.StatusFailed, rpt.Status)
	})
	t.Run("binaries still differ after install", func(t *testing.T) {
		rpt, _ := run(t, true, [][]string{{"0.18.7"}, {"0.18.7"}}, func(m *software.MockSoftware) {
			m.EXPECT().Download()
			m.EXPECT().Extract()
			m.EXPECT().Install()
			m.EXPECT().Configure()
			m.EXPECT().Cleanup()
		})
		assert.Equal(t, automa.StatusFailed, rpt.Status)
		assert.Contains(t, rpt.Error.Error(), "do not match 0.20.1 after the upgrade")
	})
	t.Run("download fails", func(t *testing.T) {
		rpt, _ := run(t, true, [][]string{{"0.18.7"}}, func(m *software.MockSoftware) {
			m.EXPECT().Download().Return(assert.AnError)
		})
		assert.Equal(t, automa.StatusFailed, rpt.Status)
	})
}
