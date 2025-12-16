// SPDX-License-Identifier: Apache-2.0

//go:build integration

package node

import (
	"testing"

	"github.com/hashgraph/solo-weaver/cmd/weaver/commands/common"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestBlocknodeInstallUpgradeCmd(t *testing.T) {
	testutil.Reset(t)

	//
	// INSTALL BLOCKNODE
	//

	cmd := testutil.PrepareSubCmdForTest(installCmd)

	// add required flags
	cmd.PersistentFlags().String(common.FlagProfileName, "", "profile to use for block commands")

	// call the subcommand explicitly to avoid test-runner arg interference
	cmd.SetArgs([]string{"install", "--profile=local", "--values=../../../../../test/config/blocknode_values.yaml"})
	err := cmd.Execute()
	require.NoError(t, err)

	//
	// UPGRADE BLOCKNODE
	//

	cmd = testutil.PrepareSubCmdForTest(upgradeCmd)

	// add required flags
	cmd.PersistentFlags().String(common.FlagProfileName, "", "profile to use for block commands")

	// call the subcommand explicitly to avoid test-runner arg interference
	cmd.SetArgs([]string{"upgrade", "--profile=local", "--values=../../../../../test/config/blocknode_values.yaml"})
	err = cmd.Execute()
	require.NoError(t, err)

	//
	// UPGRADE BLOCKNODE with no-reuse-values
	//

	cmd = testutil.PrepareSubCmdForTest(upgradeCmd)

	// add required flags
	cmd.PersistentFlags().String(common.FlagProfileName, "", "profile to use for block commands")

	// call the subcommand explicitly to avoid test-runner arg interference
	cmd.SetArgs([]string{"upgrade", "--profile=local", "--values=../../../../../test/config/blocknode_values.yaml", "--no-reuse-values"})
	err = cmd.Execute()
	require.NoError(t, err)

	//
	// UPGRADE BLOCKNODE without custom values file (pure value reuse)
	// This follows Helm CLI convention: helm upgrade release chart --reuse-values
	//

	cmd = testutil.PrepareSubCmdForTest(upgradeCmd)

	// add required flags
	cmd.PersistentFlags().String(common.FlagProfileName, "", "profile to use for block commands")

	// call the subcommand explicitly to avoid test-runner arg interference
	cmd.SetArgs([]string{"upgrade", "--profile=local"})
	err = cmd.Execute()
	require.NoError(t, err)
}
