// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"

	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// verifyExecutables is the time-of-use checksum check used by every step in this
// package. It is a package var so tests can stub the verification result without
// a real catalog or on-disk binaries.
var verifyExecutables = software.VerifyExecutables

// VerifyExecutablesStep verifies the named artifact's installed binaries against
// their catalog checksums. Insert it before SetupSystemdService for
// systemd-launched binaries (kubelet, cri-o + runtimes, teleport): it is their
// only time-of-use check, so tampering blocks the service start.
func VerifyExecutablesStep(artifactName string) *automa.StepBuilder {
	stepId := fmt.Sprintf("verify-executables-%s", artifactName)

	return automa.NewStepBuilder().WithId(stepId).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Verifying %s binaries", artifactName))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, fmt.Sprintf("Checksum verification failed for %s binaries", artifactName))
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, fmt.Sprintf("%s binaries verified", artifactName))
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if err := verifyExecutables(artifactName); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			return automa.SuccessReport(stp)
		})
}
