// SPDX-License-Identifier: Apache-2.0

// Integration tests for cluster health checks that require a running cluster.
//
// Build Tag: require_cluster
//
// These tests run in Phase 2 of the Taskfile `test:integration:verbose` task,
// after the cluster has been created in Phase 1. See internal/workflows/cluster_it_test.go
// for the full execution flow documentation.
//
// Dependencies:
//   - Requires a running Kubernetes cluster (created by Phase 1)
//   - Requires valid kubeconfig
//
// To run standalone: go test -v -tags='require_cluster' ./internal/workflows/steps/...

//go:build require_cluster

package steps

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckClusterHealth_Integration(t *testing.T) {
	s, err := CheckClusterHealth().Build()
	require.NoError(t, err)

	// Workflow's Prepare must be called to inject the kube client into context
	ctx, err := s.Prepare(context.Background())
	require.NoError(t, err)

	report := s.Execute(ctx)
	require.NotNil(t, report)
	require.NoError(t, report.Error)
}
