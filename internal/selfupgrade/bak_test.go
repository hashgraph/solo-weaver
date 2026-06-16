// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package selfupgrade_test

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/selfupgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBakName_Produce(t *testing.T) {
	cliName, err := selfupgrade.CLIBakName("op123")
	require.NoError(t, err)
	assert.Equal(t, "solo-provisioner-op123.bak", cliName)

	daemonName, err := selfupgrade.DaemonBakName("op123")
	require.NoError(t, err)
	assert.Equal(t, "solo-provisioner-daemon-op123.bak", daemonName)

	bakDir := selfupgrade.BakDir("/opt/solo/weaver/backup")
	assert.Equal(t, "/opt/solo/weaver/backup/solo-provisioner", bakDir)

	cliPath, err := selfupgrade.CLIBakPath(bakDir, "op123")
	require.NoError(t, err)
	assert.Equal(t, "/opt/solo/weaver/backup/solo-provisioner/solo-provisioner-op123.bak", cliPath)

	daemonPath, err := selfupgrade.DaemonBakPath(bakDir, "op123")
	require.NoError(t, err)
	assert.Equal(t, "/opt/solo/weaver/backup/solo-provisioner/solo-provisioner-daemon-op123.bak", daemonPath)
}

func TestBakName_RoundTrip(t *testing.T) {
	const opID = "op-2026-06-16-abc123"

	cliName, err := selfupgrade.CLIBakName(opID)
	require.NoError(t, err)
	cliBin, cliOp, err := selfupgrade.ParseBakName(cliName)
	require.NoError(t, err)
	assert.Equal(t, selfupgrade.BinaryCLI, cliBin)
	assert.Equal(t, opID, cliOp)

	daemonName, err := selfupgrade.DaemonBakName(opID)
	require.NoError(t, err)
	dBin, dOp, err := selfupgrade.ParseBakName(daemonName)
	require.NoError(t, err)
	assert.Equal(t, selfupgrade.BinaryDaemon, dBin)
	assert.Equal(t, opID, dOp)
}

func TestBakName_RejectsUnsafeOperationID(t *testing.T) {
	// A traversing / non-identifier operationId must be rejected by every
	// producer so it can never reach the filesystem as a root-written path.
	for _, bad := range []string{"../evil", "a/b", "a..b", "", "op id", "op;rm"} {
		if _, err := selfupgrade.CLIBakName(bad); err == nil {
			t.Errorf("CLIBakName(%q) should have errored", bad)
		}
		if _, err := selfupgrade.DaemonBakName(bad); err == nil {
			t.Errorf("DaemonBakName(%q) should have errored", bad)
		}
		if _, err := selfupgrade.CLIBakPath("/opt/solo/weaver/backup/solo-provisioner", bad); err == nil {
			t.Errorf("CLIBakPath(%q) should have errored", bad)
		}
		if _, err := selfupgrade.DaemonBakPath("/opt/solo/weaver/backup/solo-provisioner", bad); err == nil {
			t.Errorf("DaemonBakPath(%q) should have errored", bad)
		}
	}
}

func TestParseBakName_DaemonPrefixWinsOverCLI(t *testing.T) {
	// The CLI name is a prefix of the daemon name; the parser must not
	// misclassify a daemon archive as a CLI archive with opID "daemon-op123".
	bin, op, err := selfupgrade.ParseBakName("solo-provisioner-daemon-op123.bak")
	require.NoError(t, err)
	assert.Equal(t, selfupgrade.BinaryDaemon, bin)
	assert.Equal(t, "op123", op)
}

func TestParseBakName_FromPath(t *testing.T) {
	bin, op, err := selfupgrade.ParseBakName("/opt/solo/weaver/bin/solo-provisioner-op123.bak")
	require.NoError(t, err)
	assert.Equal(t, selfupgrade.BinaryCLI, bin)
	assert.Equal(t, "op123", op)
}

func TestParseBakName_Errors(t *testing.T) {
	cases := map[string]string{
		"missing suffix":    "solo-provisioner-op123",
		"wrong prefix":      "some-other-binary-op123.bak",
		"empty cli opID":    "solo-provisioner-.bak",
		"empty daemon opID": "solo-provisioner-daemon-.bak",
		"not a binary":      "random.bak",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := selfupgrade.ParseBakName(input)
			require.Error(t, err, "expected parse error for %q", input)
		})
	}
}
