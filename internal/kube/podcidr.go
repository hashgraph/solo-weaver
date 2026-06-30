// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"context"
	"os"

	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// DetectNodePodCIDR returns the pod CIDR assigned to the local node by reading
// its .spec.podCIDR from the Kubernetes API. The local node is identified by the
// OS hostname; when no node name matches the hostname but the cluster has
// exactly one node, that node is used (the common single-node block-node host).
//
// It returns an error when the cluster is unreachable, the local node cannot be
// identified, or the node has no pod CIDR assigned. Callers treating detection
// as best-effort should fall back on error (e.g. omit the pod-scoped rule).
func (c *Client) DetectNodePodCIDR(ctx context.Context) (string, error) {
	list, err := c.List(ctx, KindNode, "", WaitOptions{})
	if err != nil {
		return "", errorx.Decorate(err, "failed to list nodes for pod-CIDR detection")
	}
	host, _ := os.Hostname()
	return selectNodePodCIDR(list.Items, host)
}

// selectNodePodCIDR picks the local node from nodes and returns its
// .spec.podCIDR. It is split out from DetectNodePodCIDR so the selection logic
// is unit-testable without a live cluster.
func selectNodePodCIDR(nodes []unstructured.Unstructured, hostname string) (string, error) {
	if len(nodes) == 0 {
		return "", errorx.IllegalState.New("cluster has no nodes")
	}

	var node *unstructured.Unstructured
	if hostname != "" {
		for i := range nodes {
			if nodes[i].GetName() == hostname {
				node = &nodes[i]
				break
			}
		}
	}
	if node == nil {
		if len(nodes) != 1 {
			return "", errorx.IllegalState.New(
				"cannot identify local node among %d nodes (hostname %q); pass --pod-cidr explicitly",
				len(nodes), hostname)
		}
		node = &nodes[0]
	}

	cidr, found, err := unstructured.NestedString(node.Object, "spec", "podCIDR")
	if err != nil {
		return "", errorx.Decorate(err, "failed to read .spec.podCIDR from node %q", node.GetName())
	}
	if !found || cidr == "" {
		return "", errorx.IllegalState.New("node %q has no .spec.podCIDR assigned", node.GetName())
	}
	return cidr, nil
}
