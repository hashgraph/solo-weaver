package steps

import (
	"context"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/kube"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

// CheckClusterConfigMaps checks if the specified config maps exist in the cluster
func CheckClusterConfigMaps(id string, configMaps []string, timeout time.Duration, provider kube.ClientProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(id).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := provider()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			for _, item := range configMaps {
				namespace, name := splitIntoNamespaceAndName(item)
				err = k.WaitForResource(ctx, kube.KindConfigMaps, namespace, name, kube.IsPresent, timeout)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
			}

			foundConfigMaps, err := prepareConfigMapMeta(ctx, k)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			meta["checkedConfigMaps"] = strings.Join(configMaps, ", ")
			meta["foundConfigMaps"] = strings.Join(foundConfigMaps, ", ")

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking cluster config maps")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to check cluster config maps")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cluster config maps are checked successfully")
		})
}

func prepareConfigMapMeta(ctx context.Context, k *kube.Client) ([]string, error) {
	items, err := k.List(ctx, kube.KindConfigMaps, "", kube.WaitOptions{})
	if err != nil {
		return nil, err
	}

	foundConfigMaps := []string{}
	for _, item := range items.Items {
		foundConfigMaps = append(foundConfigMaps, strings.TrimSpace(
			item.GetNamespace()+"/"+item.GetName()+"-"+item.GetCreationTimestamp().String(),
		))
	}
	return foundConfigMaps, nil
}
