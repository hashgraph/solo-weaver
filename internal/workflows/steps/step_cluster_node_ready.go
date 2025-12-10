// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// CheckClusterNodesReady checks if all nodes in the cluster are ready
func CheckClusterNodesReady(id string, provider kube.ClientProviderFromContext) automa.Builder {
	return automa.NewStepBuilder().WithId(id).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			err = k.WaitForResources(ctx, kube.KindNode, "", kube.IsNodeReady, 30*time.Second, kube.WaitOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			nodeInfo, err := prepareNodeInfoForMeta(ctx, k)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			meta := map[string]string{
				"nodeCount": fmt.Sprintf("%d", len(nodeInfo)),
				"nodes":     strings.Join(nodeInfo, ", "),
			}

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking if all cluster nodes are ready")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to check if all cluster nodes are ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "All cluster nodes are ready")
		})
}

func prepareNodeInfoForMeta(ctx context.Context, k *kube.Client) ([]string, error) {
	var nodeInfo []string

	items, err := k.List(ctx, kube.KindNode, "", kube.WaitOptions{})
	if err != nil {
		return nil, err
	}

	for _, item := range items.Items {
		conditions, found, err := unstructured.NestedSlice(item.Object, "status", "conditions")
		if err != nil || !found {
			continue
		}

		var readyStatus string
		for _, condition := range conditions {
			condMap, ok := condition.(map[string]interface{})
			if !ok {
				continue
			}
			if condMap["type"] == "Ready" {
				readyStatus = fmt.Sprintf("%v", condMap["status"])
				break
			}
		}

		nodeInfo = append(nodeInfo, fmt.Sprintf("%s(Ready=%s)", item.GetName(), readyStatus))
	}

	return nodeInfo, nil
}
