// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"testing"

	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyCmd_FromFolder(t *testing.T) {
	dir := genFolder(t)
	out, err := runUnderNode(t, verifyCmd, "--keys-dir", dir)
	require.NoError(t, err)
	assert.Contains(t, out, "All checks passed")
}

func TestVerifyCmd_RequiresSource(t *testing.T) {
	_, err := runUnderNode(t, verifyCmd)
	require.Error(t, err)
}

func TestVerifyCmd_FromCluster(t *testing.T) {
	dir := genFolder(t)
	fake := &fakeSecretClient{store: map[string]map[string][]byte{}}
	stubSecretClient(t, fake)

	// Seed the cluster.
	_, err := runUnderNode(t, importCmd, "--namespace", "ns", "--node-id", "0", "--keys-dir", dir)
	require.NoError(t, err)

	out, err := runUnderNode(t, verifyCmd, "--from-cluster", "--namespace", "ns", "--node-id", "0")
	require.NoError(t, err)
	assert.Contains(t, out, "All checks passed")
}

func TestVerifyCmd_FromClusterMissing(t *testing.T) {
	stubSecretClient(t, &fakeSecretClient{store: map[string]map[string][]byte{}})
	_, err := runUnderNode(t, verifyCmd, "--from-cluster", "--namespace", "ns", "--node-id", "0", "--gossip")
	require.Error(t, err)
}

// ensure the fake satisfies the interface used by the orchestration.
var _ cnkeys.SecretClient = (*fakeSecretClient)(nil)
