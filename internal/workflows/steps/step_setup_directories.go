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

			// rootOwnedSet holds dirs that must stay root:root 0755 — never the
			// root:weaver 2775 (setgid, group-writable) treatment used for the
			// provisioner home dirs.
			//
			// Two distinct reasons land dirs here:
			//   - Sandbox dirs are bind-mounted into system containers (e.g. cilium's
			//     mount-cgroup init container writes to SandboxBinDir via /hostbin).
			//     Those containers run as root but are not in the hedera group.
			//   - BinDir holds the solo-provisioner CLI, which sudoers grants weaver
			//     passwordless root to exec. If BinDir were group-writable by weaver,
			//     any weaver-group member could unlink the root-owned binary and drop
			//     a replacement, turning the NOPASSWD grant into a local root
			//     escalation. Only root writes here (install/upgrade/self-upgrade all
			//     run as root), so root:root 0755 is correct and loses nothing.
			//
			// Enforcing this on every run also remediates existing installations that
			// were provisioned before BinDir was excluded from the root:weaver bucket.
			rootOwnedSet := make(map[string]struct{}, len(pp.SandboxDirectories)+1)
			rootOwnedSet[pp.BinDir] = struct{}{}
			for _, d := range pp.SandboxDirectories {
				rootOwnedSet[d] = struct{}{}
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
				_, isRootOwned := rootOwnedSet[dir]
				if isRootOwned {
					if err = mg.WritePermissions(dir, models.DefaultDirOrExecPerm, false); err != nil {
						return automa.FailureReport(stp, automa.WithError(err))
					}
					// Explicitly reassert root:root — a pre-existing dir (e.g. BinDir
					// from an older install) may currently be root:weaver, and leaving
					// the group ownership in place would keep the escalation open.
					if err = mg.WriteOwnerByName(dir, "root", "root", false); err != nil {
						return automa.FailureReport(stp, automa.WithError(err))
					}
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
