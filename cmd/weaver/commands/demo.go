// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/spf13/cobra"
)

var flagDemoStress bool

var tuiDemoCmd = &cobra.Command{
	Use:    "tui-demo",
	Short:  "Run a fake workflow to exercise the TUI rendering",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagDemoStress {
			common.RunBuilderWorkflow(cmd.Context(), demoStressWorkflow())
		} else {
			common.RunBuilderWorkflow(cmd.Context(), demoWorkflow())
		}
		return nil
	},
}

func init() {
	tuiDemoCmd.Flags().BoolVar(&flagDemoStress, "stress", false, "Run many phases/steps to test TUI overflow")
	common.SkipGlobalChecks(tuiDemoCmd)
}

// demoWorkflow builds a fake workflow with three phases to exercise all TUI rendering paths.
func demoWorkflow() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("tui-demo").
		Steps(
			demoPreflightPhase(),
			demoSetupPhase(),
			demoDeployPhase(),
		)
}

// demoPreflightPhase: 3 steps — success, success, skipped.
func demoPreflightPhase() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("demo-preflight").
		Steps(
			demoStep("check-privileges", "Checking privileges", 400*time.Millisecond, statusOK),
			demoStep("check-hardware", "Validating hardware", 500*time.Millisecond, statusOK),
			demoStep("check-network", "Checking network", 300*time.Millisecond, statusSkip),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "Preflight Checks")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "Preflight Checks")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "Preflight Checks")
		})
}

// demoSetupPhase: 3 steps with detail messages — all succeed.
func demoSetupPhase() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("demo-setup").
		Steps(
			demoStepWithDetail("install-packages", "Installing system packages", 800*time.Millisecond,
				[]string{"Updating package index...", "Installing iptables...", "Installing conntrack..."}),
			demoStep("configure-kernel", "Configuring kernel modules", 600*time.Millisecond, statusOK),
			demoStepWithDetail("setup-services", "Setting up services", 700*time.Millisecond,
				[]string{"Enabling nftables...", "Starting containerd...", "Configuring kubelet..."}),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "System Setup")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "System Setup")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "System Setup")
		})
}

// demoDeployPhase: 2 steps — one succeeds, one fails.
func demoDeployPhase() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().
		WithId("demo-deploy").
		Steps(
			demoStep("pull-images", "Pulling container images", 500*time.Millisecond, statusOK),
			demoStep("deploy-chart", "Deploying Helm chart", 400*time.Millisecond, statusFail),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().PhaseStart(ctx, stp, "Deploy")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseFailure(ctx, stp, rpt, "Deploy")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().PhaseCompletion(ctx, stp, rpt, "Deploy")
		})
}

type demoOutcome int

const (
	statusOK demoOutcome = iota
	statusFail
	statusSkip
)

// demoStep creates a fake step that sleeps and returns the given outcome.
func demoStep(id, name string, duration time.Duration, outcome demoOutcome) *automa.StepBuilder {
	return automa.NewStepBuilder().
		WithId(id).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, name)
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			time.Sleep(duration)
			switch outcome {
			case statusFail:
				return automa.FailureReport(stp, automa.WithError(fmt.Errorf("simulated failure in %s", name)))
			case statusSkip:
				return automa.SkippedReport(stp)
			default:
				return automa.SuccessReport(stp)
			}
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, name)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, name)
		})
}

// demoStepWithDetail creates a fake step that emits detail messages while sleeping.
func demoStepWithDetail(id, name string, totalDuration time.Duration, details []string) *automa.StepBuilder {
	return automa.NewStepBuilder().
		WithId(id).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, name)
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			interval := totalDuration / time.Duration(len(details)+1)
			for _, d := range details {
				time.Sleep(interval)
				notify.As().StepDetail(ctx, stp, d)
			}
			time.Sleep(interval)
			return automa.SuccessReport(stp)
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, name)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, name)
		})
}

// demoStressWorkflow creates a large workflow (~35 steps across 6 phases) to test TUI overflow.
func demoStressWorkflow() *automa.WorkflowBuilder {
	phases := []struct {
		id, name string
		steps    int
	}{
		{"stress-preflight", "Preflight Checks", 7},
		{"stress-system", "System Setup", 8},
		{"stress-kubernetes", "Kubernetes Setup", 6},
		{"stress-networking", "Networking", 5},
		{"stress-storage", "Storage Setup", 5},
		{"stress-deploy", "Deploy", 4},
	}

	var builders []automa.Builder
	for _, p := range phases {
		var steps []automa.Builder
		for i := 1; i <= p.steps; i++ {
			stepName := fmt.Sprintf("%s step %d", p.name, i)
			stepID := fmt.Sprintf("%s-%d", p.id, i)
			details := []string{
				fmt.Sprintf("checking %s...", stepName),
				fmt.Sprintf("installing %s...", stepName),
				fmt.Sprintf("configuring %s...", stepName),
			}
			steps = append(steps, demoStepWithDetail(stepID, stepName, 300*time.Millisecond, details))
		}
		phaseName := p.name
		builders = append(builders, automa.NewWorkflowBuilder().
			WithId(p.id).
			Steps(steps...).
			WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
				notify.As().PhaseStart(ctx, stp, phaseName)
				return ctx, nil
			}).
			WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
				notify.As().PhaseFailure(ctx, stp, rpt, phaseName)
			}).
			WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
				notify.As().PhaseCompletion(ctx, stp, rpt, phaseName)
			}))
	}

	return automa.NewWorkflowBuilder().
		WithId("tui-stress").
		Steps(builders...)
}
