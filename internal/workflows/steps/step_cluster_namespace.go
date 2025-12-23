// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
)

// CheckClusterNamespaces checks if the specified namespaces exist in the cluster
// namespaces is a list of namespace names
func CheckClusterNamespaces(id string, namespaces []string, timeout time.Duration, provider kube.ClientProviderFromContext) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(id).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			for _, ns := range namespaces {
				err = k.WaitForResource(ctx, kube.KindNamespace, "", ns, kube.IsPresent, timeout)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
			}

			foundNamespaces, err := prepareNamespaceMeta(ctx, k)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{}
			meta["checkedNamespaces"] = strings.Join(namespaces, ", ")
			meta["foundNamespaces"] = strings.Join(foundNamespaces, ", ")

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking cluster namespaces")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to check cluster namespaces")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cluster namespaces checked successfully")
		})
}

func prepareNamespaceMeta(ctx context.Context, k *kube.Client) ([]string, error) {
	items, err := k.List(ctx, kube.KindNamespace, "", kube.WaitOptions{})
	if err != nil {
		return nil, err
	}

	foundNamespaces := []string{}
	for _, item := range items.Items {
		foundNamespaces = append(foundNamespaces, item.GetName())
	}
	return foundNamespaces, nil
}
