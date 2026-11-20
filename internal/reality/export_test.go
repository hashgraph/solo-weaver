// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// UseClusterInfo makes the cluster checker read info, or fail with err, instead
// of asking a live API server, until the test ends.
func UseClusterInfo(t interface{ Cleanup(func()) }, info *models.ClusterInfo, err error) {
	orig := retrieveClusterInfo
	t.Cleanup(func() { retrieveClusterInfo = orig })
	retrieveClusterInfo = func() (*models.ClusterInfo, error) { return info, err }
}

// ReadCluster runs the node and Cilium agent read of the cluster checker on r.
func ReadCluster(ctx context.Context, r ClusterReader, cs *state.ClusterState) error {
	c := &clusterChecker{newReader: func() (ClusterReader, error) { return r, nil }}
	return c.readCluster(ctx, cs)
}
