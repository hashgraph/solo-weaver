//go:build integration

package blockcmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlocknodeCheckCmd(t *testing.T) {
	cmd := prepareSubCmdForTest(blockNodeCheckCmd)

	// call the subcommand explicitly to avoid test-runner arg interference
	cmd.SetArgs([]string{"check", "--profile=local"})
	err := cmd.Execute()
	require.NoError(t, err)
}
