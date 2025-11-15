package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-provisioner/internal/kube"
	"golang.hedera.com/solo-provisioner/internal/workflows/notify"
)

// CheckClusterCRDs checks if the specified CRDs are installed in the cluster
// crds is a list of CRD names
func CheckClusterCRDs(id string, crds []string, timeout time.Duration, provider kube.ClientProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(id).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := provider()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			for _, name := range crds {
				err = k.WaitForResource(ctx, kube.KindCRD, "", name, kube.IsCRDReady, timeout)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
			}

			// update meta with the checked CRDs and their details
			foundCRDs, err := prepareCRDMeta(ctx, k)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(fmt.Errorf("failed to prepare CRD meta: %w", err)))
			}

			meta := map[string]string{}
			meta["checkedCRDs"] = strings.Join(crds, ", ")
			meta["foundCRDs"] = strings.Join(foundCRDs, ", ")

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking cluster CRDs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to check cluster CRDs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cluster CRDs checked successfully")
		})
}

func prepareCRDMeta(ctx context.Context, k *kube.Client) ([]string, error) {
	items, err := k.List(ctx, kube.KindCRD, "", kube.WaitOptions{})
	if err != nil {
		return nil, err
	}

	foundCRDs := []string{}
	for _, item := range items.Items {
		foundCRDs = append(foundCRDs, strings.TrimSpace(
			item.GetNamespace()+"/"+item.GetName()+"-"+item.GetCreationTimestamp().String(),
		))
	}
	return foundCRDs, nil
}
