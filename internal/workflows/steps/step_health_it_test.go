//go:build helm_integration || require_cluster

package steps

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckClusterHealth_Integration(t *testing.T) {
	s, err := CheckClusterHealth().Build()
	require.NoError(t, err)
	report := s.Execute(context.Background())
	require.NotNil(t, report)
	require.NoError(t, report.Error)
}
