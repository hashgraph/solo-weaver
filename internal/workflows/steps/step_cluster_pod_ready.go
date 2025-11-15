package steps

import (
	"context"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/kube"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

// CheckClusterPodsReady checks if the specified pods are running in the cluster
// pods is a map of pod prefixes to namespace
func CheckClusterPodsReady(id string, podNames []string, timeout time.Duration, provider kube.ClientProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(id).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := provider()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			for _, item := range podNames {
				namespace, namePrefix := splitIntoNamespaceAndName(item)
				err = k.WaitForResources(ctx, kube.KindPod, namespace, kube.IsPodReady, timeout, kube.WaitOptions{NamePrefix: namePrefix})
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
			}

			foundPods, err := preparePodMeta(ctx, k)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			meta["checkedPods"] = strings.Join(podNames, ", ")
			meta["foundPods"] = strings.Join(foundPods, ", ")

			// get all namespaces in the cluster
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking if all cluster pods are running")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to check if all cluster pods are running")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "All cluster pods are running")
		})
}

func preparePodMeta(ctx context.Context, k *kube.Client) ([]string, error) {
	items, err := k.List(ctx, kube.KindPod, "", kube.WaitOptions{})
	if err != nil {
		return nil, err
	}

	foundPods := []string{}
	for _, item := range items.Items {
		foundPods = append(foundPods, item.GetNamespace()+"/"+item.GetName())
	}
	return foundPods, nil
}
