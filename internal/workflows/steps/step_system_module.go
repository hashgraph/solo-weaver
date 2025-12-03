// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"

	"golang.hedera.com/solo-weaver/internal/workflows/notify"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-weaver/pkg/kernel"
)

// InstallKernelModule ensures that a specific kernel module is loaded and persisted.
// If the module is already loaded, it skips the loading process.
// On rollback, it unloads the module only if it was loaded by this step.
func InstallKernelModule(name string) automa.Builder {
	stepId := fmt.Sprintf("load-kernel-module-%s", name)
	loadedByThisStep := false

	return automa.NewStepBuilder().WithId(stepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			module, err := kernel.NewModule(name)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(
					automa.StepExecutionError.Wrap(err, "failed to load kernel module: %s", name)))
			}

			alreadyLoaded, alreadyPersisted, err := module.IsLoaded()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err,
							"failed to check if module %s is loaded", name)))
			}

			// Prepare metadata for reporting
			meta := map[string]string{
				"module":           name,
				"loadedByThisStep": fmt.Sprintf("%t", loadedByThisStep),
				"alreadyLoaded":    fmt.Sprintf("%t", alreadyLoaded),
				"alreadyPersisted": fmt.Sprintf("%t", alreadyPersisted),
			}

			if alreadyLoaded && alreadyPersisted {
				return automa.SuccessReport(stp, automa.WithMetadata(meta))
			}

			err = module.Load(true)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err,
							"failed to load and persist kernel module: %s", name)))
			}

			loadedByThisStep = true
			meta[string(LoadedByThisStep)] = fmt.Sprintf("%t", loadedByThisStep)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			module, err := kernel.NewModule(name)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(
					automa.StepExecutionError.Wrap(err, "failed to load kernel module: %s", name)))
			}

			if !loadedByThisStep {
				return automa.SkippedReport(stp)
			}

			err = module.Unload(true)
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(
						automa.StepExecutionError.Wrap(err,
							"failed to unload kernel module: %s", name)))
			}

			return automa.SuccessReport(stp)
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepFailure(ctx, stp, report,
				fmt.Sprintf("Failed to load kernel module: %s", name))
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, report *automa.Report) {
			notify.As().StepCompletion(ctx, stp, report,
				fmt.Sprintf("Kernel module %s loaded successfully", name))
		})
}
