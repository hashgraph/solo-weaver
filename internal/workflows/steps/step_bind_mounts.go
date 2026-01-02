// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"path"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/mount"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
)

const (
	KeyModifiedByThisStep = "modifiedByThisStep"
	KeyBindTarget         = "bindTarget"
	KeyBindMount          = "bindMount"
	KeyAlreadyMounted     = "alreadyMounted"
	KeyAlreadyInFstab     = "alreadyInFstab"
)

func SetupBindMounts() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("setup-bind-mounts").
		Steps(
			setupBindMount("kubernetes", "/etc/kubernetes"),
			setupBindMount("kubelet", "/var/lib/kubelet"),
			setupBindMount("cilium", "/var/run/cilium"),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up bind mounts")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup bind mounts")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Bind mounts setup successfully")
		})
}

func setupBindMount(name string, target string) automa.Builder {
	return automa.NewStepBuilder().WithId(fmt.Sprintf("setup-bind-mount-for-%s", name)).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Setting up bind mount - %s", target))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, fmt.Sprintf("Failed to setup bind mount - %s", target))
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, fmt.Sprintf("Bind mount setup successfully - %s", target))
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			bindMount := mount.BindMount{
				Source: path.Join(core.Paths().SandboxDir, target),
				Target: target,
			}
			stp.State().Set(KeyBindMount, bindMount)

			modifiedByThisStep := false

			// Check if already bind mounted and recorded in fstab
			alreadyMounted, alreadyInFstab, err := mount.IsBindMountedWithFstab(bindMount)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}
			stp.State().Set(KeyAlreadyMounted, alreadyMounted)
			stp.State().Set(KeyAlreadyInFstab, alreadyInFstab)

			// Prepare metadata for reporting
			meta := map[string]string{
				KeyBindMount:          name,
				KeyBindTarget:         target,
				KeyModifiedByThisStep: fmt.Sprintf("%t", modifiedByThisStep),
				KeyAlreadyMounted:     fmt.Sprintf("%t", alreadyMounted),
				KeyAlreadyInFstab:     fmt.Sprintf("%t", alreadyInFstab),
			}

			if alreadyMounted && alreadyInFstab {
				return automa.SuccessReport(stp, automa.WithMetadata(meta))
			}

			err = mount.SetupBindMountsWithFstab(bindMount)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			modifiedByThisStep = true
			stp.State().Set(KeyModifiedByThisStep, modifiedByThisStep)
			meta[KeyModifiedByThisStep] = fmt.Sprintf("%t", modifiedByThisStep)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			alreadyMounted := stp.State().Bool(KeyAlreadyMounted)
			alreadyInFstab := stp.State().Bool(KeyAlreadyInFstab)

			if alreadyMounted && alreadyInFstab {
				return automa.SkippedReport(stp, automa.WithDetail("bind mount was not modified by this step, skipping rollback"))
			}

			var bindMount mount.BindMount
			if val, ok := stp.State().Get(KeyBindMount); ok {
				bindMount = val.(mount.BindMount)
			}

			// Check if already bind mounted and recorded in fstab
			mounted, inFstab, err := mount.IsBindMountedWithFstab(bindMount)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			if !alreadyMounted && mounted || !alreadyInFstab && inFstab {
				err = mount.RemoveBindMountsWithFstab(bindMount)
				if err != nil {
					return automa.FailureReport(stp,
						automa.WithError(err))
				}
			}

			return automa.SuccessReport(stp)
		})
}

// TeardownBindMounts removes bind mounts and their fstab entries
func TeardownBindMounts() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("teardown-bind-mounts").
		Steps(
			teardownBindMount("kubernetes", "/etc/kubernetes"),
			teardownBindMount("kubelet", "/var/lib/kubelet"),
			teardownBindMount("cilium", "/var/run/cilium"),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing bind mounts")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove bind mounts")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Bind mounts removed successfully")
		})
}

func teardownBindMount(name string, target string) automa.Builder {
	return automa.NewStepBuilder().WithId(fmt.Sprintf("teardown-bind-mount-for-%s", name)).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Removing bind mount - %s", target))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, fmt.Sprintf("Failed to remove bind mount - %s", target))
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, fmt.Sprintf("Bind mount removed successfully - %s", target))
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			bindMount := mount.BindMount{
				Source: path.Join(core.Paths().SandboxDir, target),
				Target: target,
			}

			err := mount.RemoveBindMountsWithFstab(bindMount)
			if err != nil {
				logx.As().Warn().Err(err).Msgf("Failed to remove bind mount %s, continuing with teardown", target)
				// Don't fail if unmount fails - mount might not exist
			}

			return automa.SuccessReport(stp)
		})
}
