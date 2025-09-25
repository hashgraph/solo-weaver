//go:build integration
// +build integration

package steps

import (
	"context"
	"fmt"
	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"testing"
)

func TestSetupClusterUsingBashCommands(t *testing.T) {
	registry, err := BashScriptBasedStepRegistry()
	require.NoError(t, err)

	wf, err := registry.Of(SetupClusterStepId).Build()
	require.NoError(t, err)

	report, err := wf.Execute(context.Background())
	require.NoError(t, err)
	b, _ := yaml.Marshal(report)
	fmt.Printf("Workflow Execution Report:%s\n", b)
	require.Equal(t, automa.StatusSuccess, report.Status)
}
