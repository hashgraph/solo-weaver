package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/pkg/kernel"
)

// InstallKernelModule ensures that a specific kernel module is loaded and persisted.
// If the module is already loaded, it skips the loading process.
// On rollback, it unloads the module only if it was loaded by this step.
func InstallKernelModule(name string) automa.Builder {
	stepId := fmt.Sprintf("load-kernel-module-%s", name)

	loadedByThisStep := false

	return automa.NewStepBuilder(stepId,
		automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
			module, err := kernel.NewModule(name)
			if err != nil {
				return nil, err
			}

			alreadyLoaded, alreadyPersisted, err := module.IsLoaded()
			if err != nil {
				return nil, fmt.Errorf("failed to check if module %s is loaded: %w", name, err)
			}
			if alreadyLoaded && alreadyPersisted {
				return automa.StepSuccessReport(stepId), nil
			}

			err = module.Load(true)
			if err != nil {
				return nil, err
			}
			loadedByThisStep = true

			return automa.StepSuccessReport(stepId), nil
		}),
		automa.WithOnRollback(func(ctx context.Context) (*automa.Report, error) {
			module, err := kernel.NewModule(name)
			if err != nil {
				return nil, err
			}

			if !loadedByThisStep {
				return automa.StepSuccessReport(stepId), nil
			}

			err = module.Unload(true)
			if err != nil {
				return nil, err
			}

			return automa.StepSuccessReport(stepId), nil
		}))
}
