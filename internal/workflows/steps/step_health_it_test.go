// SPDX-License-Identifier: Apache-2.0

// Integration tests for cluster health checks that require a running cluster.
//
// Build Tag: require_cluster
// Naming Convention: All tests are prefixed with TestWithCluster_
//
// These tests run in Phase 3 of the Taskfile `test:integration:verbose` task,
// after the cluster has been created in Phase 2. See internal/workflows/cluster_it_test.go
// for the full execution flow documentation.
//
// Dependencies:
//   - Requires a running Kubernetes cluster (created by Phase 2)
//   - Requires valid kubeconfig
//
// To run standalone: go test -v -tags='require_cluster' -run '^TestWithCluster_' ./internal/workflows/steps/...

//go:build require_cluster

package steps

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithCluster_CheckClusterHealth_Integration(t *testing.T) {
	s, err := CheckClusterHealth().Build()
	require.NoError(t, err)

	// Workflow's Prepare must be called to inject the kube client into context
	ctx, err := s.Prepare(context.Background())
	require.NoError(t, err)

	report := s.Execute(ctx)
	require.NotNil(t, report)
	require.NoError(t, report.Error)
}
