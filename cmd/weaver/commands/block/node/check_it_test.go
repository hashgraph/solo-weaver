//go:build integration

package node

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/internal/testutil"
)

func TestBlocknodeCheckCmd(t *testing.T) {
	cmd := testutil.PrepareSubCmdForTest(checkCmd)

	// add required flags
	cmd.PersistentFlags().String("profile", "", "profile to use for block commands")

	// call the subcommand explicitly to avoid test-runner arg interference
	cmd.SetArgs([]string{"check", "--profile=local"})
	err := cmd.Execute()
	require.NoError(t, err)
}
