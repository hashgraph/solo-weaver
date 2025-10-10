package steps

import (
	"context"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
)

func SetupHomeDirectoryStructure(pp *core.ProvisionerPaths) automa.Builder {
	return automa.NewStepBuilder().WithId("home_directories").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if pp == nil {
				return automa.FailureReport(stp, automa.WithError(errorx.IllegalArgument.New("provisioner path is nil")))
			}

			mg, err := fsx.NewManager()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			for _, dir := range pp.AllDirectories {
				_, exists, err := mg.PathExists(dir)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				} else if exists {
					// directory already exists, skip
					continue
				}

				err = mg.CreateDirectory(dir, true)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}

				err = mg.WritePermissions(dir, core.DefaultFilePerm, true)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}
			}

			// add metadata about created directories
			return automa.SuccessReport(stp, automa.WithMetadata(map[string]string{
				"directories": strings.Join(pp.AllDirectories, ", "),
			}))
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup home directory structure")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Home directory structure setup successfully")
		})
}
