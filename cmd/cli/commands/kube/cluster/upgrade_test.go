// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/bll/clusterupgrade"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
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

// forceForTests backs the root --force flag, which the command reads through
// its parents.
var forceForTests bool

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
	t.Cleanup(func() { planLive = orig })
	out, err := execUpgrade(t, args)
	return out, got, err
}

// execUpgrade parses args into the command and runs its RunE, resetting every
// flag afterwards.
func execUpgrade(t *testing.T, args []string) (string, error) {
	t.Helper()
	if clusterCmd.PersistentFlags().Lookup(common.FlagForce().Name) == nil {
		common.FlagForce().SetVarP(clusterCmd, &forceForTests, true)
	}
	t.Cleanup(func() {
		flagUpgradePlan, flagUpgradeTo, flagUpgradeAll, flagUpgradeDryRun = false, "", false, false
		flagUpgradeStopOnError, flagUpgradeRollbackOnError, flagUpgradeContinueOnError, forceForTests = false, false, false, false
		upgradeCmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
		common.OutputFormat = ""
	})
	require.NoError(t, upgradeCmd.ParseFlags(args))
	if err := upgradeCmd.ValidateFlagGroups(); err != nil {
		return "", err
	}
	var out bytes.Buffer
	upgradeCmd.SetOut(&out)
	t.Cleanup(func() { upgradeCmd.SetOut(nil) })
	err := upgradeCmd.RunE(upgradeCmd, nil)
	return out.String(), err
}

// stubApply makes the command's apply return res and err, and keeps the
// options it was called with.
func stubApply(t *testing.T, res clusterupgrade.ApplyResult, err error) *clusterupgrade.ApplyOptions {
	t.Helper()
	got := &clusterupgrade.ApplyOptions{}
	orig := applyLive
	applyLive = func(_ context.Context, opts clusterupgrade.ApplyOptions) (clusterupgrade.ApplyResult, error) {
		*got = opts
		return res, err
	}
	t.Cleanup(func() { applyLive = orig })
	return got
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

func TestUpgrade_AppliesTheNextStep(t *testing.T) {
	plan := v035Plan()
	got := stubApply(t, clusterupgrade.ApplyResult{Outcome: clusterupgrade.OutcomeApplied, Target: plan.Target,
		Applied: plan.Hops[:1], Remaining: plan.Hops[1:]}, nil)

	out, err := execUpgrade(t, []string{"--to", "1.35", "--force"})

	require.NoError(t, err)
	assert.Equal(t, "1.35", got.To)
	assert.False(t, got.All)
	assert.Nil(t, got.Confirm, "--force skips the question")
	assert.Contains(t, out, "Cluster upgrade: 1 step applied (applied)")
	assert.Contains(t, out, "1. cilium 1.18.1 -> 1.18.16")
	assert.Contains(t, out, "Next: cilium 1.18.16 -> 1.19.10 (8 steps left)")
}

func TestUpgrade_AllWalksEveryStep(t *testing.T) {
	plan := v035Plan()
	got := stubApply(t, clusterupgrade.ApplyResult{Outcome: clusterupgrade.OutcomeWalked, Target: plan.Target,
		Applied: plan.Hops, Remaining: []clusterupgrade.Hop{}}, nil)

	out, err := execUpgrade(t, []string{"--all", "--force"})

	require.NoError(t, err)
	assert.True(t, got.All)
	assert.Contains(t, out, "9 steps applied (walked)")
	assert.Contains(t, out, "The cluster is at the target (Kubernetes 1.36.5, Cilium 1.20.2)")
}

func TestUpgrade_FailedStepPrintsWhatWasApplied(t *testing.T) {
	plan := v035Plan()
	stubApply(t, clusterupgrade.ApplyResult{Outcome: clusterupgrade.OutcomeFailed, Target: plan.Target,
		Applied: plan.Hops[:1], Remaining: plan.Hops[1:], Cause: "'cilium upgrade' to 1.19.10 failed"},
		errx.Decorate(errorx.ExternalError.New("'cilium upgrade' to 1.19.10 failed"), reasons.PreconditionNotMet))

	out, err := execUpgrade(t, []string{"--all", "--force"})

	require.Error(t, err)
	reason, _ := errx.ReasonOf(err)
	assert.Equal(t, reasons.PreconditionNotMet, reason, "the step's own reason is kept")
	assert.Contains(t, out, "1 step applied (failed)")
	assert.Contains(t, out, "Stopped: 'cilium upgrade' to 1.19.10 failed")
}

func TestUpgrade_ApplyJSONEndsWithTheResult(t *testing.T) {
	common.OutputFormat = "json"
	plan := v035Plan()
	stubApply(t, clusterupgrade.ApplyResult{Outcome: clusterupgrade.OutcomeApplied, Target: plan.Target,
		Applied: plan.Hops[:1], Remaining: plan.Hops[1:]}, nil)

	out, err := execUpgrade(t, []string{"--force"})

	require.NoError(t, err)
	var got struct {
		Type    string                 `json:"type"`
		Outcome clusterupgrade.Outcome `json:"outcome"`
		Applied []clusterupgrade.Hop   `json:"applied"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "upgrade", got.Type)
	assert.Equal(t, clusterupgrade.OutcomeApplied, got.Outcome)
	assert.Len(t, got.Applied, 1)
}

func TestUpgrade_RefusesRollbackAndContinueOnError(t *testing.T) {
	for _, flag := range []string{"--rollback-on-error", "--continue-on-error"} {
		t.Run(flag, func(t *testing.T) {
			got := stubApply(t, clusterupgrade.ApplyResult{}, nil)
			_, err := execUpgrade(t, []string{flag})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "supports only --stop-on-error")
			reason, _ := errx.ReasonOf(err)
			assert.Equal(t, reasons.PreconditionNotMet, reason)
			assert.Equal(t, clusterupgrade.ApplyOptions{}, *got, "nothing runs")
		})
	}
	assert.True(t, upgradeCmd.Flags().Lookup("rollback-on-error").Hidden)
	assert.True(t, upgradeCmd.Flags().Lookup("continue-on-error").Hidden)
	assert.False(t, upgradeCmd.Flags().Lookup("stop-on-error").Hidden)
}

func TestUpgrade_StopOnErrorIsAccepted(t *testing.T) {
	stubApply(t, clusterupgrade.ApplyResult{Outcome: clusterupgrade.OutcomeAtTarget}, nil)
	out, err := execUpgrade(t, []string{"--stop-on-error", "--force"})
	require.NoError(t, err)
	assert.Contains(t, out, "Nothing to do")
}

func TestUpgrade_ModesAreExclusive(t *testing.T) {
	for _, args := range [][]string{{"--plan", "--all"}, {"--plan", "--dry-run"}, {"--dry-run", "--all"}, {"--list", "--all"}} {
		_, err := execUpgrade(t, args)
		require.Error(t, err, args)
	}
}

func TestUpgrade_DryRun(t *testing.T) {
	plan := v035Plan()
	next := plan.Hops[0]
	orig := dryRunLive
	t.Cleanup(func() { dryRunLive = orig })

	t.Run("passing checks", func(t *testing.T) {
		dryRunLive = func(context.Context, clusterupgrade.Options) (clusterupgrade.DryRunResult, error) {
			return clusterupgrade.DryRunResult{Plan: plan, Next: &next, Checks: clusterupgrade.Checks{{Name: "one node", OK: true}}}, nil
		}
		out, err := execUpgrade(t, []string{"--dry-run"})
		require.NoError(t, err)
		assert.Contains(t, out, "Next step: cilium 1.18.1 -> 1.18.16 (patch)")
		assert.Contains(t, out, "[ok]     one node")
	})
	t.Run("an internal error keeps its reason", func(t *testing.T) {
		dryRunLive = func(context.Context, clusterupgrade.Options) (clusterupgrade.DryRunResult, error) {
			return clusterupgrade.DryRunResult{}, errx.Decorate(errorx.IllegalArgument.New("no checks for \"x\" hops"), reasons.Internal)
		}
		_, err := execUpgrade(t, []string{"--dry-run"})
		require.Error(t, err)
		reason, _ := errx.ReasonOf(err)
		assert.Equal(t, reasons.Internal, reason)
	})
	t.Run("a failed check", func(t *testing.T) {
		dryRunLive = func(context.Context, clusterupgrade.Options) (clusterupgrade.DryRunResult, error) {
			return clusterupgrade.DryRunResult{Plan: plan, Next: &next,
				Checks: clusterupgrade.Checks{{Name: "one node", Detail: "the cluster has 3 nodes"}}}, nil
		}
		out, err := execUpgrade(t, []string{"--dry-run"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "1 check(s) failed")
		assert.Contains(t, out, "[failed] one node: the cluster has 3 nodes")
	})
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
