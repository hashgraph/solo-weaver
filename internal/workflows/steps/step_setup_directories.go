// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"

	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
)

func SetupHomeDirectoryStructure(pp models.WeaverPaths) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("home_directories").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			mg, err := fsx.NewManager()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			// Sandbox dirs are bind-mounted into system containers (e.g. cilium's
			// mount-cgroup init container writes to SandboxBinDir via /hostbin).
			// Those containers run as root but are not in the hedera group, so
			// sandbox dirs must stay root:root 0755 — not hedera:hedera 2775.
			sandboxSet := make(map[string]struct{}, len(pp.SandboxDirectories))
			for _, d := range pp.SandboxDirectories {
				sandboxSet[d] = struct{}{}
			}

			for _, dir := range pp.AllDirectories {
				_, exists, err := mg.PathExists(dir)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}

				if !exists {
					if err = mg.CreateDirectory(dir, true); err != nil {
						return automa.FailureReport(stp, automa.WithError(err))
					}
				}

				// Always enforce permissions, even on pre-existing dirs.
				// Each dir is processed individually by this loop, so recursive=false is
				// correct — it avoids bluntly chmod-ing file contents to a directory mode.
				_, isSandbox := sandboxSet[dir]
				if isSandbox {
					if err = mg.WritePermissions(dir, models.DefaultDirOrExecPerm, false); err != nil {
						return automa.FailureReport(stp, automa.WithError(err))
					}
					// No chown — sandbox dirs must stay root:root.
				} else {
					// Provisioner home dirs are owned root:weaver with setgid so the
					// weaver service can write and new files inherit the weaver group.
					// hedera ownership is reserved for block-node storage only.
					if err = mg.WritePermissions(dir, models.DefaultStorageDirPerm, false); err != nil {
						return automa.FailureReport(stp, automa.WithError(err))
					}
					if err = mg.WriteOwnerByName(dir, "root", config.WeaverGroupName(), false); err != nil {
						return automa.FailureReport(stp, automa.WithError(err))
					}
				}
			}

			// add metadata about created directories
			return automa.SuccessReport(stp, automa.WithMetadata(map[string]string{
				"directories": strings.Join(pp.AllDirectories, ", "),
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up home directory structure")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup home directory structure")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Home directory structure setup successfully")
		})
}
