// SPDX-License-Identifier: Apache-2.0

package reality

import "github.com/hashgraph/solo-weaver/pkg/models"

// UseClusterInfo makes the cluster checker read info, or fail with err, instead
// of asking a live API server, until the test ends.
func UseClusterInfo(t interface{ Cleanup(func()) }, info *models.ClusterInfo, err error) {
	orig := retrieveClusterInfo
	t.Cleanup(func() { retrieveClusterInfo = orig })
	retrieveClusterInfo = func() (*models.ClusterInfo, error) { return info, err }
}
