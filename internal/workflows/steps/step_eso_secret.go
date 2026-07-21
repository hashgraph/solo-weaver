// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

const (
	SetupESOSecretStepId  = "setup-eso-secret"
	CreateESOSecretStepId = "create-eso-secret"

	esoExternalSecretTemplatePath = "files/eso/external-secret.yaml"
)

// esoSecretManifestFilePath is where the rendered ExternalSecret manifest is
// written before it is applied to the cluster.
var esoSecretManifestFilePath = path.Join(models.Paths().TempDir, "eso-external-secret.yaml")

// ESOSecretOptions parameterizes the ExternalSecret create workflow. Its exported
// fields are consumed directly by the manifest template.
type ESOSecretOptions struct {
	Name            string
	Namespace       string
	StoreName       string
	RefreshInterval string
	Data            []models.ESOSecretDataEntry
}

// SetupESOSecret returns a workflow builder that applies an ExternalSecret to the
// cluster from the given options.
func SetupESOSecret(opts ESOSecretOptions) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId(SetupESOSecretStepId).Steps(
		createESOSecret(opts),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating ExternalSecret")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create ExternalSecret")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "ExternalSecret created successfully")
		})
}

// renderESOSecretManifest renders the ExternalSecret manifest for the given
// options. Extracted so it can be unit-tested without a cluster.
func renderESOSecretManifest(opts ESOSecretOptions) (string, error) {
	return templates.Render(esoExternalSecretTemplatePath, opts)
}

func createESOSecret(opts ESOSecretOptions) automa.Builder {
	return automa.NewStepBuilder().WithId(CreateESOSecretStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			rendered, err := renderESOSecretManifest(opts)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.InternalError.Wrap(err, "failed to render ExternalSecret manifest").
						WithProperty(models.ErrPropertyResolution, []string{
							"Check the solo-provisioner logs for details: /opt/solo/weaver/logs/solo-provisioner.log",
						})))
			}

			if err := os.WriteFile(esoSecretManifestFilePath, []byte(rendered), models.DefaultFilePerm); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.ExternalError.Wrap(err, "failed to write ExternalSecret manifest to %s", esoSecretManifestFilePath).
						WithProperty(models.ErrPropertyResolution, []string{
							"Check free disk space and write permissions for " + models.Paths().TempDir,
						})))
			}

			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.ExternalError.Wrap(err, "failed to initialise Kubernetes client").
						WithProperty(models.ErrPropertyResolution, []string{
							"Verify the cluster is reachable: kubectl cluster-info",
							"Ensure the Kubernetes cluster is installed: solo-provisioner kube cluster install",
						})))
			}

			if err := k.ApplyManifest(ctx, esoSecretManifestFilePath); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(
					errorx.ExternalError.Wrap(err, "failed to apply ExternalSecret %q in namespace %q", opts.Name, opts.Namespace).
						WithProperty(models.ErrPropertyResolution, []string{
							"Ensure the External Secrets Operator is installed: solo-provisioner eso operator install",
							"Ensure the namespace exists: kubectl get namespace " + opts.Namespace,
							"If the error mentions the ESO webhook, wait for the operator to become ready and retry",
							"Inspect the rendered manifest: " + esoSecretManifestFilePath,
						})))
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{"created": "true"}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Applying ExternalSecret %s", opts.Name)
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to apply ExternalSecret")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "ExternalSecret applied successfully")
		})
}
