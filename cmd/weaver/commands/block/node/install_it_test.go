//go:build integration

package node

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/testutil"
)

func TestBlocknodeInstallCmd(t *testing.T) {
	cmd := testutil.PrepareSubCmdForTest(installCmd)

	// add required flags
	cmd.PersistentFlags().String("profile", "", "profile to use for block commands")

	// call the subcommand explicitly to avoid test-runner arg interference
	cmd.SetArgs([]string{"install", "--profile=local", "--values=../../../../../test/config/blocknode_values.yaml"})
	err := cmd.Execute()
	require.NoError(t, err)
}
