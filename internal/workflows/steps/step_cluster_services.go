package steps

import (
	"context"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-weaver/internal/kube"
	"golang.hedera.com/solo-weaver/internal/workflows/notify"
)

// CheckClusterServices checks if the specified services are running in the cluster
// services is a list of strings in the format 'namespace/service-name'
func CheckClusterServices(id string, services []string, timeout time.Duration, provider kube.ClientProviderFromContext) automa.Builder {
	return automa.NewStepBuilder().WithId(id).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			for _, item := range services {
				namespace, name := splitIntoNamespaceAndName(item)
				err = k.WaitForResource(ctx, kube.KindService, namespace, name, kube.IsPresent, timeout)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
			}

			foundServices, err := prepareServiceMeta(ctx, k)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			meta["checkedServices"] = strings.Join(services, ", ")
			meta["foundServices"] = strings.Join(foundServices, ", ")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking cluster services")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to check cluster services")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cluster services checked successfully")
		})
}

func prepareServiceMeta(ctx context.Context, k *kube.Client) ([]string, error) {
	items, err := k.List(ctx, kube.KindService, "", kube.WaitOptions{})
	if err != nil {
		return nil, err
	}

	foundServices := []string{}
	for _, item := range items.Items {
		foundServices = append(foundServices, item.GetNamespace()+"/"+item.GetName())
	}
	return foundServices, nil
}
