//go:build integration

package blockcmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlocknodeInstallCmd(t *testing.T) {
	cmd := prepareSubCmdForTest(blockNodeInstallCmd)

	// call the subcommand explicitly to avoid test-runner arg interference
	cmd.SetArgs([]string{"install", "--profile=local"})
	err := cmd.Execute()
	require.NoError(t, err)
}
