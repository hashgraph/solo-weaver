// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/joomcode/errorx"
)

// EnsureWeaverOwnerStep idempotently creates the weaver:2500 user and group when they do not
// exist. This runs as the first step of self-install so that SetupHomeDirectoryStructure can
// chown the provisioner home dirs to weaver:weaver.
func EnsureWeaverOwnerStep() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("ensure-weaver-owner").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			pm, err := principal.NewManager()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			weaverGroupName := config.WeaverGroupName()
			weaverGroupId := config.WeaverGroupId()
			weaverUserName := config.WeaverUserName()
			weaverUserId := config.WeaverUserId()

			meta := map[string]string{}

			// --- Group ---
			weaverGroup, groupErr := pm.LookupGroupByName(weaverGroupName)
			if groupErr != nil {
				gid, err := strconv.Atoi(weaverGroupId)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(
						errorx.IllegalState.Wrap(err, "invalid weaver owner group ID: %s", weaverGroupId)))
				}
				if _, err := pm.CreateGroupWithId(weaverGroupName, gid); err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}
				logx.As().Info().Msgf("Created group %s:%s", weaverGroupName, weaverGroupId)
				meta["weaver_group_created"] = "true"
			} else {
				if weaverGroup.Gid() != weaverGroupId {
					instructions := fmt.Sprintf(
						"Group %q exists with GID %s but GID %s is required.\n"+
							"Fix it with: sudo groupmod -g %s %s",
						weaverGroupName, weaverGroup.Gid(), weaverGroupId, weaverGroupId, weaverGroupName)
					return automa.FailureReport(stp,
						automa.WithError(errorx.IllegalState.New(
							"weaver owner group %q has incorrect GID: expected %s, got %s",
							weaverGroupName, weaverGroupId, weaverGroup.Gid())),
						automa.WithMetadata(map[string]string{"instructions": instructions}))
				}
				meta["weaver_group_created"] = "false"
			}

			// --- User ---
			weaverUser, userErr := pm.LookupUserByName(weaverUserName)
			if userErr != nil {
				uid, err := strconv.Atoi(weaverUserId)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(
						errorx.IllegalState.Wrap(err, "invalid weaver owner user ID: %s", weaverUserId)))
				}
				if _, err := pm.CreateUserWithId(weaverUserName, uid, config.WeaverHomeDir()); err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}
				logx.As().Info().Msgf("Created user %s:%s", weaverUserName, weaverUserId)
				meta["weaver_user_created"] = "true"
			} else {
				if weaverUser.Uid() != weaverUserId {
					instructions := fmt.Sprintf(
						"User %q exists with UID %s but UID %s is required.\n"+
							"Fix it with: sudo usermod -u %s %s",
						weaverUserName, weaverUser.Uid(), weaverUserId, weaverUserId, weaverUserName)
					return automa.FailureReport(stp,
						automa.WithError(errorx.IllegalState.New(
							"weaver owner user %q has incorrect UID: expected %s, got %s",
							weaverUserName, weaverUserId, weaverUser.Uid())),
						automa.WithMetadata(map[string]string{"instructions": instructions}))
				}
				meta["weaver_user_created"] = "false"
			}

			// --- Home directory ---
			// Always ensure /home/weaver exists with correct ownership, whether the
			// account was just created or already existed. The weaver daemon needs a
			// real home directory so systemd can resolve $HOME and the k8s client can
			// find ~/.kube/config.
			mg, err := fsx.NewManager()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			_, homeDirExists, err := mg.PathExists(config.WeaverHomeDir())
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			if !homeDirExists {
				if err = mg.CreateDirectory(config.WeaverHomeDir(), false); err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}
			}
			homeUser, err := pm.LookupUserByName(weaverUserName)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.IllegalState.Wrap(err, "failed to lookup weaver user after creation")))
			}
			homeGroup, err := pm.LookupGroupByName(weaverGroupName)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.IllegalState.Wrap(err, "failed to lookup weaver group after creation")))
			}
			if err = mg.WriteOwner(config.WeaverHomeDir(), homeUser, homeGroup, false); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			if err = mg.WritePermissions(config.WeaverHomeDir(), os.FileMode(0750), false); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			meta["weaver_home_ensured"] = "true"

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Ensuring weaver service account (weaver:2500)")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to ensure weaver service account")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Weaver service account ensured")
		})
}
