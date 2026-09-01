// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"
	"path"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

const (
	SetupESOSecretStepId    = "setup-eso-secret"
	CreateESOSecretStepId   = "create-eso-secret"
	TeardownESOSecretStepId = "teardown-eso-secret"
	DeleteESOSecretStepId   = "delete-eso-secret"

	esoExternalSecretTemplatePath = "files/eso/external-secret.yaml"

	// The resource the delete step targets; matches the rendered template.
	esoExternalSecretAPIVersion = "external-secrets.io/v1"
	esoExternalSecretKind       = "ExternalSecret"
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

// TeardownESOSecret returns a workflow builder that deletes the named
// ExternalSecret from the cluster. Kubernetes garbage-collects the Secret it
// synced, because the manifest sets target.creationPolicy: Owner.
func TeardownESOSecret(name, namespace string) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId(TeardownESOSecretStepId).Steps(
		deleteESOSecret(name, namespace),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Deleting ExternalSecret")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to delete ExternalSecret")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "ExternalSecret deleted successfully")
		})
}

func deleteESOSecret(name, namespace string) automa.Builder {
	return automa.NewStepBuilder().WithId(DeleteESOSecretStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to initialise Kubernetes client"),
					reasons.PreconditionNotMet,
					"Verify the cluster is reachable: kubectl cluster-info",
					"Ensure the Kubernetes cluster is installed: solo-provisioner kube cluster install",
				)))
			}

			deleted, err := k.DeleteResource(ctx, esoExternalSecretAPIVersion, esoExternalSecretKind, namespace, name)
			if err != nil {
				// NewClient never dials the API server, so an unreachable cluster first
				// surfaces here, in discovery. Reachability leads the hints for that reason.
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to delete ExternalSecret %q in namespace %q", name, namespace),
					reasons.PreconditionNotMet,
					"Verify the cluster is reachable: kubectl cluster-info",
					fmt.Sprintf("Check whether it exists: kubectl get externalsecret %s -n %s", name, namespace),
					"Ensure the namespace exists: kubectl get namespace "+namespace,
					`If the error mentions no matches for kind "ExternalSecret", the External Secrets Operator is not installed: solo-provisioner eso operator install`,
				)))
			}

			if !deleted {
				l.Info().Msg("ExternalSecret does not exist, skipping deletion")
				return automa.StepSkippedReport(stp.Id())
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{"deleted": "true"}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Deleting ExternalSecret %s", name)
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to delete ExternalSecret")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "ExternalSecret deleted successfully")
		})
}
