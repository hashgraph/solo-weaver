// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll/clusterupgrade"
)

func v035Plan() clusterupgrade.Plan {
	k := func(step clusterupgrade.HopStep, from, to string) clusterupgrade.Hop {
		return clusterupgrade.Hop{Kind: clusterupgrade.KindKubernetes, Step: step, Component: "kubernetes", From: from, To: to}
	}
	c := func(step clusterupgrade.HopStep, from, to string) clusterupgrade.Hop {
		return clusterupgrade.Hop{Kind: clusterupgrade.KindCilium, Step: step, Component: "cilium", From: from, To: to}
	}
	return clusterupgrade.Plan{
		Target: clusterupgrade.Target{Kubernetes: "1.36.5", Cilium: "1.20.2"},
		Hops: []clusterupgrade.Hop{
			c(clusterupgrade.StepPatch, "1.18.1", "1.18.16"),
			c(clusterupgrade.StepMinor, "1.18.16", "1.19.10"),
			k(clusterupgrade.StepPatch, "1.33.4", "1.33.13"),
			k(clusterupgrade.StepMinor, "1.33.13", "1.34.13"),
			c(clusterupgrade.StepMinor, "1.19.10", "1.20.2"),
			k(clusterupgrade.StepMinor, "1.34.13", "1.35.10"),
			{Kind: clusterupgrade.KindCharts, Step: clusterupgrade.StepCoupled, Kubernetes: "1.36",
				Charts: []clusterupgrade.ChartBump{{Chart: "metallb", From: "0.15.2", To: "0.16.1"}}},
			k(clusterupgrade.StepMinor, "1.35.10", "1.36.5"),
			{Kind: clusterupgrade.KindCharts, Step: clusterupgrade.StepRefresh, Charts: []clusterupgrade.ChartBump{
				{Chart: "alloy", From: "1.4.0", To: "1.13.1"}, {Chart: "metrics-server", From: "3.13.0", To: "3.14.0"}}},
		},
	}
}

func TestRenderPlan_FullLadder(t *testing.T) {
	want := "Cluster upgrade plan: 9 steps remaining (target: Kubernetes 1.36.5, Cilium 1.20.2)\n" +
		"\n" +
		"  Version ladder:\n" +
		"  [next]  1. Cilium          1.18.1  -> 1.18.16  (patch)\n" +
		"          2. Cilium          1.18.16 -> 1.19.10  (minor)\n" +
		"          3. Kubernetes      1.33.4  -> 1.33.13  (patch)\n" +
		"          4. Kubernetes      1.33.13 -> 1.34.13  (minor)\n" +
		"          5. Cilium          1.19.10 -> 1.20.2   (minor)\n" +
		"          6. Kubernetes      1.34.13 -> 1.35.10  (minor)\n" +
		"          7. metallb         0.15.2  -> 0.16.1   (needed by Kubernetes 1.36)\n" +
		"          8. Kubernetes      1.35.10 -> 1.36.5   (minor)\n" +
		"\n" +
		"  Charts refresh:\n" +
		"          9. alloy           1.4.0   -> 1.13.1   (catalog default)\n" +
		"             metrics-server  3.13.0  -> 3.14.0\n"
	assert.Equal(t, want, renderPlan(v035Plan()))
}

func TestRenderPlan_StructuralAndResume(t *testing.T) {
	plan := clusterupgrade.Plan{
		Target: clusterupgrade.Target{Kubernetes: "1.36.5", Cilium: "1.20.2"},
		Hops: []clusterupgrade.Hop{
			{Kind: clusterupgrade.KindKubernetes, Step: clusterupgrade.StepResume, Component: "kubernetes", From: "1.35.10", To: "1.36.5"},
			{Kind: clusterupgrade.KindStructural, Step: clusterupgrade.StepReplace, Component: "node-exporter", From: "4.5.19", To: "4.59.0"},
			{Kind: clusterupgrade.KindStructural, Step: clusterupgrade.StepMigrate, Component: "external-secrets", From: "1.0.0", To: "2.13.0"},
		},
	}
	want := "Cluster upgrade plan: 3 steps remaining (target: Kubernetes 1.36.5, Cilium 1.20.2)\n" +
		"\n" +
		"  Interrupted Kubernetes upgrade:\n" +
		"  [next]  1. Kubernetes        1.35.10 -> 1.36.5  (finish the kubelet)\n" +
		"\n" +
		"  Structural migrations:\n" +
		"          2. node-exporter     4.5.19  -> 4.59.0  (chart replacement)\n" +
		"          3. external-secrets  1.0.0   -> 2.13.0  (CRD API migration)\n"
	assert.Equal(t, want, renderPlan(plan))
}

func TestRenderPlan_OneStep(t *testing.T) {
	plan := clusterupgrade.Plan{
		Target: clusterupgrade.Target{Kubernetes: "1.36.5", Cilium: "1.20.2"},
		Hops:   []clusterupgrade.Hop{{Kind: clusterupgrade.KindCilium, Step: clusterupgrade.StepPatch, Component: "cilium", From: "1.20.1", To: "1.20.2"}},
	}
	assert.Contains(t, renderPlan(plan), "Cluster upgrade plan: 1 step remaining")
}

func TestRenderPlan_NothingToDo(t *testing.T) {
	plan := clusterupgrade.Plan{Target: clusterupgrade.Target{Kubernetes: "1.36.5", Cilium: "1.20.2"}}
	assert.Equal(t, "Nothing to do: the cluster is at or past the target (Kubernetes 1.36.5, Cilium 1.20.2).\n", renderPlan(plan))
}

func TestRenderPlan_Blockers(t *testing.T) {
	plan := clusterupgrade.Plan{
		Target: clusterupgrade.Target{Kubernetes: "1.36.5"},
		Blockers: []clusterupgrade.Blocker{
			{Code: clusterupgrade.BlockerNotObserved, Detail: "the cluster could not be fully read"},
			{Code: clusterupgrade.BlockerCiliumMismatch, Component: "cilium", Detail: "the Cilium chart is 1.20.2 but the agent runs 1.19.10"},
		},
	}
	want := "Cannot plan the upgrade to Kubernetes 1.36.5:\n" +
		"  - not-observed: the cluster could not be fully read\n" +
		"  - cilium-mismatch: the Cilium chart is 1.20.2 but the agent runs 1.19.10\n"
	assert.Equal(t, want, renderPlan(plan))
}

// runUpgrade runs the command's RunE with a stubbed planner. The experimental
// gate and the root pre-run are not part of RunE.
func runUpgrade(t *testing.T, args []string, plan clusterupgrade.Plan, planErr error) (string, clusterupgrade.Options, error) {
	t.Helper()
	var got clusterupgrade.Options
	orig := planLive
	planLive = func(_ context.Context, opts clusterupgrade.Options) (clusterupgrade.Plan, error) {
		got = opts
		return plan, planErr
	}
	t.Cleanup(func() {
		planLive = orig
		flagUpgradePlan, flagUpgradeTo = false, ""
		common.OutputFormat = ""
	})

	require.NoError(t, upgradeCmd.ParseFlags(args))
	var out bytes.Buffer
	upgradeCmd.SetOut(&out)
	t.Cleanup(func() { upgradeCmd.SetOut(nil) })
	err := upgradeCmd.RunE(upgradeCmd, nil)
	return out.String(), got, err
}

func TestUpgrade_PlanPrintsThePlan(t *testing.T) {
	for _, flag := range []string{"--plan", "--list"} {
		t.Run(flag, func(t *testing.T) {
			out, opts, err := runUpgrade(t, []string{flag, "--to", "1.35"}, v035Plan(), nil)
			require.NoError(t, err)
			assert.Equal(t, clusterupgrade.Options{To: "1.35"}, opts)
			assert.Contains(t, out, "Cluster upgrade plan: 9 steps remaining")
		})
	}
}

func TestUpgrade_WithoutPlanIsRefused(t *testing.T) {
	_, _, err := runUpgrade(t, nil, v035Plan(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "applying upgrade steps is not available yet")
}

func TestUpgrade_BlockersPrintAndFail(t *testing.T) {
	plan := clusterupgrade.Plan{
		Target:   clusterupgrade.Target{Kubernetes: "1.36.5"},
		Blockers: []clusterupgrade.Blocker{{Code: clusterupgrade.BlockerMultipleNodes, Detail: "the cluster has 3 nodes"}},
	}
	out, _, err := runUpgrade(t, []string{"--plan"}, plan, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be planned: 1 blocker(s)")
	assert.Contains(t, out, "multiple-nodes: the cluster has 3 nodes")
}

func TestUpgrade_JSONOutput(t *testing.T) {
	common.OutputFormat = "json"
	out, _, err := runUpgrade(t, []string{"--plan"}, v035Plan(), nil)
	require.NoError(t, err)

	var got clusterupgrade.Plan
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, v035Plan(), got)
	assert.Contains(t, out, `"kubernetes": "1.36"`)
}

func TestUpgrade_PlanErrorsAreDecorated(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"bad target", errorx.IllegalArgument.New("bad --to"), "InvalidArgument"},
		{"bad catalog", errorx.IllegalFormat.New("bad matrix"), "Internal"},
		{"read failure", errorx.IllegalState.New("no cluster"), "StateOperationFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := runUpgrade(t, []string{"--plan"}, clusterupgrade.Plan{}, tt.err)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestUpgrade_IsBehindTheExperimentalGate(t *testing.T) {
	assert.True(t, upgradeCmd.Hidden)
	assert.NotNil(t, upgradeCmd.PersistentFlags().Lookup("experimental"))
	assert.Contains(t, clusterCmd.Commands(), upgradeCmd)
}
