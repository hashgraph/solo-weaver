// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/core"
)

func TestSetupHomeDirectoryStructure_Integration(t *testing.T) {
	// Use a real temp directory as the home
	tmpHome := t.TempDir()
	pp := core.NewWeaverPaths(tmpHome)

	step, err := SetupHomeDirectoryStructure(pp).Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())

	require.Equal(t, automa.StatusSuccess, report.Status)
	require.NoError(t, report.Error)

	// Check that all expected directories exist
	for _, dir := range pp.AllDirectories {
		info, err := os.Stat(dir)
		require.NoErrorf(t, err, "directory %s should exist", dir)
		require.Truef(t, info.IsDir(), "%s should be a directory", dir)
		// Optionally, check permissions here if needed
	}
}
