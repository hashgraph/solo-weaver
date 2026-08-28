// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

const PrecheckConsensusSecretsStepId = "precheck-consensus-secrets"

func PrecheckConsensusSecrets(inputs models.ConsensusNodeInputs) automa.Builder {
	return automa.NewStepBuilder().WithId(PrecheckConsensusSecretsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			secrets := map[string]string{
				"--grpc-tls-secret": inputs.GrpcTlsSecret,
				"--signing-secret":  inputs.SigningSecret,
				"--hapi-app-secret": inputs.HapiAppSecret,
			}

			kc, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			for flag, name := range secrets {
				exists, err := kc.ResourceExists(ctx, "v1", "Secret", inputs.Namespace, name)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(err, "failed to check Secret %s", name),
						reasons.PreconditionNotMet,
						"Verify cluster connectivity and that your kubeconfig has RBAC to read Secrets")))
				}
				if !exists {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.New(
							"Secret %q (from %s) not found in namespace %s — create it before installing",
							name, flag, inputs.Namespace),
						reasons.PreconditionNotMet,
						fmt.Sprintf("Create the required Kubernetes Secret %q in namespace %s (e.g. via 'solo-provisioner eso secret create') before installing", name, inputs.Namespace))))
				}
				logx.As().Info().Str("secret", name).Str("flag", flag).Msg("Secret exists")
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Validating secret references in namespace %s", inputs.Namespace))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Secret validation failed")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Secret references validated")
		})
}
