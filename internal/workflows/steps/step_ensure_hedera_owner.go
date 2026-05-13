// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strconv"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/joomcode/errorx"
)

// EnsureHederaOwnerStep idempotently creates the hedera:2000 user and group when they do not
// exist, then adds the weaver service account to the hedera group so it can write to block-node
// storage directories (which are setgid hedera:hedera 2775).
func EnsureHederaOwnerStep() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("ensure-hedera-owner").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			pm, err := principal.NewManager()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			hederaGroupName := config.HederaGroupName()
			hederaGroupId := config.HederaGroupId()
			hederaUserName := config.HederaUserName()
			hederaUserId := config.HederaUserId()
			weaverUserName := config.WeaverUserName()

			meta := map[string]string{}

			// --- Group ---
			hederaGroup, groupErr := pm.LookupGroupByName(hederaGroupName)
			if groupErr != nil {
				// Group does not exist — create it.
				gid, err := strconv.Atoi(hederaGroupId)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(
						errorx.IllegalState.Wrap(err, "invalid hedera owner group ID: %s", hederaGroupId)))
				}
				if _, err := pm.CreateGroupWithId(hederaGroupName, gid); err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}
				logx.As().Info().Msgf("Created group %s:%s", hederaGroupName, hederaGroupId)
				meta["hedera_group_created"] = "true"
			} else {
				// Group exists — verify GID matches.
				if hederaGroup.Gid() != hederaGroupId {
					instructions := fmt.Sprintf(
						"Group %q exists with GID %s but GID %s is required.\n"+
							"Fix it with: sudo groupmod -g %s %s",
						hederaGroupName, hederaGroup.Gid(), hederaGroupId, hederaGroupId, hederaGroupName)
					return automa.FailureReport(stp,
						automa.WithError(errorx.IllegalState.New(
							"hedera owner group %q has incorrect GID: expected %s, got %s",
							hederaGroupName, hederaGroupId, hederaGroup.Gid())),
						automa.WithMetadata(map[string]string{"instructions": instructions}))
				}
				meta["hedera_group_created"] = "false"
			}

			// --- User ---
			hederaUser, userErr := pm.LookupUserByName(hederaUserName)
			if userErr != nil {
				// User does not exist — create it.
				uid, err := strconv.Atoi(hederaUserId)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(
						errorx.IllegalState.Wrap(err, "invalid hedera owner user ID: %s", hederaUserId)))
				}
				if _, err := pm.CreateUserWithId(hederaUserName, uid, "/"); err != nil {
					return automa.FailureReport(stp, automa.WithError(err))
				}
				logx.As().Info().Msgf("Created user %s:%s", hederaUserName, hederaUserId)
				meta["hedera_user_created"] = "true"
			} else {
				// User exists — verify UID matches.
				if hederaUser.Uid() != hederaUserId {
					instructions := fmt.Sprintf(
						"User %q exists with UID %s but UID %s is required.\n"+
							"Fix it with: sudo usermod -u %s %s",
						hederaUserName, hederaUser.Uid(), hederaUserId, hederaUserId, hederaUserName)
					return automa.FailureReport(stp,
						automa.WithError(errorx.IllegalState.New(
							"hedera owner user %q has incorrect UID: expected %s, got %s",
							hederaUserName, hederaUserId, hederaUser.Uid())),
						automa.WithMetadata(map[string]string{"instructions": instructions}))
				}
				meta["hedera_user_created"] = "false"
			}

			// Add weaver to the hedera group so it can write to setgid block-node storage dirs.
			if err := pm.AddUserToGroup(weaverUserName, hederaGroupName); err != nil {
				instructions := fmt.Sprintf(
					"Could not add %q to group %q.\n"+
						"Fix it with: sudo usermod -aG %s %s",
					weaverUserName, hederaGroupName, hederaGroupName, weaverUserName)
				return automa.FailureReport(stp,
					automa.WithError(errorx.IllegalState.Wrap(err,
						"failed to add %s to group %s", weaverUserName, hederaGroupName)),
					automa.WithMetadata(map[string]string{"instructions": instructions}))
			}
			logx.As().Info().Msgf("Ensured %s is a member of group %s", weaverUserName, hederaGroupName)
			meta["weaver_in_hedera_group"] = "true"

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Ensuring hedera owner (hedera:2000)")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to ensure hedera owner")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Hedera owner ensured")
		})
}
