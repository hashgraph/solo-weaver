// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_bandwidthManagerEnabled covers the decision the StartCilium guard makes on
// the cilium-config enable-bandwidth-manager value to assert the BN traffic
// shaper precondition (Bandwidth Manager off, issue #741). The ConfigMap value is
// the load-bearing contract, so these cases pin how it maps to enabled/disabled.
func Test_bandwidthManagerEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "true", value: "true", want: true},
		{name: "mixed case true", value: "True", want: true},
		{name: "true with whitespace", value: "  true ", want: true},
		{name: "false", value: "false", want: false},
		{name: "absent key (empty)", value: "", want: false},
		{name: "disabled literal", value: "disabled", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bandwidthManagerEnabled(tt.value))
		})
	}
}

const verifiedChartDir = "/opt/solo/weaver/downloads/charts/cilium-1.20.2/cilium"

// ciliumCNIHarness records every cilium CLI command, binary verification and
// chart download. `cilium status` fails, as it does on a cluster without Cilium.
type ciliumCNIHarness struct {
	scripts  []string
	verified *[]string
	charts   []ciliumRelease
	chartErr error
}

func newCiliumCNIHarness(t *testing.T, verify func(string) error) *ciliumCNIHarness {
	t.Helper()
	h := &ciliumCNIHarness{}
	h.verified = stubVerifyExecutables(t, verify)
	origRun, origFetch := runCiliumScript, fetchCiliumChart
	runCiliumScript = func(scripts []string, _ string) (string, error) {
		joined := strings.Join(scripts, " ")
		if strings.HasSuffix(joined, "cilium status") {
			return "", fmt.Errorf("cilium is not installed")
		}
		h.scripts = append(h.scripts, joined)
		return "", nil
	}
	fetchCiliumChart = func(_ context.Context, release ciliumRelease) (string, error) {
		h.charts = append(h.charts, release)
		if h.chartErr != nil {
			return "", h.chartErr
		}
		return verifiedChartDir, nil
	}
	t.Cleanup(func() { runCiliumScript, fetchCiliumChart = origRun, origFetch })
	return h
}

func TestInstallCiliumCNI_FreshClusterInstallsPinnedVersion(t *testing.T) {
	h := newCiliumCNIHarness(t, func(string) error { return nil })

	step, err := installCiliumCNI(newClusterCilium).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())

	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	assert.Equal(t, []string{"cilium"}, *h.verified)
	assert.Equal(t, []ciliumRelease{newClusterCilium}, h.charts)
	require.Len(t, h.scripts, 1)
	assert.Contains(t, h.scripts[0], `cilium install --wait --version "1.20.2" --chart-directory `+verifiedChartDir+` --values `)
	installed, _ := step.State().Local().Bool(InstalledByThisStep)
	assert.True(t, installed)
}

func TestInstallCiliumCNI_UnverifiedChartIsNotInstalled(t *testing.T) {
	h := newCiliumCNIHarness(t, func(string) error { return nil })
	h.chartErr = assert.AnError

	step, err := installCiliumCNI(newClusterCilium).Build()
	require.NoError(t, err)
	report := step.Execute(context.Background())

	require.ErrorIs(t, report.Error, assert.AnError)
	assert.Equal(t, automa.StatusFailed, report.Status)
	assert.Empty(t, h.scripts, "cilium install must not run without a verified chart")
	installed, _ := step.State().Local().Bool(InstalledByThisStep)
	assert.False(t, installed)

	rollback := step.Rollback(context.Background())
	assert.Equal(t, automa.StatusSkipped, rollback.Status)
	assert.Empty(t, h.scripts)
}

// The step verifies the cilium binary before shelling out to it on both install
// and rollback, so a failed checksum aborts before the binary is executed.

func TestInstallCiliumCNI_FailsWhenCiliumVerificationFails(t *testing.T) {
	h := newCiliumCNIHarness(t, func(string) error { return assert.AnError })

	step, err := installCiliumCNI(newClusterCilium).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	require.ErrorIs(t, report.Error, assert.AnError)
	assert.Equal(t, []string{"cilium"}, *h.verified)
	assert.Empty(t, h.charts)
	assert.Empty(t, h.scripts)
}

func TestInstallCiliumCNI_RollbackFailsWhenCiliumVerificationFails(t *testing.T) {
	h := newCiliumCNIHarness(t, func(string) error { return assert.AnError })

	step, err := installCiliumCNI(newClusterCilium).Build()
	require.NoError(t, err)

	// Rollback is a no-op unless this step performed the install; mark it so
	// rollback proceeds past that guard and reaches the checksum verification.
	step.State().Local().Set(InstalledByThisStep, true)

	report := step.Rollback(context.Background())
	require.Error(t, report.Error)
	assert.Equal(t, automa.StatusFailed, report.Status)
	require.ErrorIs(t, report.Error, assert.AnError)
	assert.Empty(t, h.scripts)
}
