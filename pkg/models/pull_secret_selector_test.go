// SPDX-License-Identifier: Apache-2.0

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePullSecretSelector(t *testing.T) {
	// Bare default applies to all; the flag's zero-arg default reproduces today's behaviour.
	sel, err := ParsePullSecretSelector([]string{"private-registry-creds"})
	require.NoError(t, err)
	assert.True(t, sel.HasDefault)
	assert.Equal(t, "private-registry-creds", sel.SecretForHost("ghcr.io"))
	assert.Equal(t, "private-registry-creds", sel.SecretForHost("anything"))

	// Host-keyed overrides plus a bare default fallback.
	sel, err = ParsePullSecretSelector([]string{"private-registry-creds", "ghcr.io=ghcr-creds", "us-docker.pkg.dev=gcr-creds"})
	require.NoError(t, err)
	assert.Equal(t, "ghcr-creds", sel.SecretForHost("ghcr.io"))
	assert.Equal(t, "gcr-creds", sel.SecretForHost("us-docker.pkg.dev"))
	assert.Equal(t, "private-registry-creds", sel.SecretForHost("docker.io"))
	assert.Equal(t, []string{"ghcr.io", "us-docker.pkg.dev"}, sel.Hosts())

	// No default supplied ⇒ unmapped hosts get no secret (public).
	sel, err = ParsePullSecretSelector([]string{"ghcr.io=ghcr-creds"})
	require.NoError(t, err)
	assert.Equal(t, "ghcr-creds", sel.SecretForHost("ghcr.io"))
	assert.Equal(t, "", sel.SecretForHost("docker.io"))

	// Bare empty ⇒ default is "public" for unmapped hosts.
	sel, err = ParsePullSecretSelector([]string{""})
	require.NoError(t, err)
	assert.True(t, sel.HasDefault)
	assert.Equal(t, "", sel.SecretForHost("ghcr.io"))

	// HOST= ⇒ explicit public pull for that one host.
	sel, err = ParsePullSecretSelector([]string{"ghcr.io="})
	require.NoError(t, err)
	assert.Equal(t, "", sel.SecretForHost("ghcr.io"))

	// Errors: more than one bare default, duplicate host, empty host.
	_, err = ParsePullSecretSelector([]string{"a", "b"})
	require.Error(t, err)
	_, err = ParsePullSecretSelector([]string{"ghcr.io=a", "ghcr.io=b"})
	require.Error(t, err)
	_, err = ParsePullSecretSelector([]string{"=a"})
	require.Error(t, err)
}

func TestNormalizeRegistrySelectionStrategy(t *testing.T) {
	// Empty stays empty (operator default).
	s, err := NormalizeRegistrySelectionStrategy("")
	require.NoError(t, err)
	assert.Equal(t, "", s)

	// Any case canonicalizes to the operator's exact enum.
	for _, in := range []string{"Random", "random", "  RANDOM "} {
		s, err = NormalizeRegistrySelectionStrategy(in)
		require.NoError(t, err)
		assert.Equal(t, "Random", s)
	}
	for _, in := range []string{"Sequential", "sequential"} {
		s, err = NormalizeRegistrySelectionStrategy(in)
		require.NoError(t, err)
		assert.Equal(t, "Sequential", s)
	}

	// Anything else is rejected.
	_, err = NormalizeRegistrySelectionStrategy("first")
	require.Error(t, err)
}

func TestRegistryHost(t *testing.T) {
	assert.Equal(t, "ghcr.io", RegistryHost("ghcr.io/hashgraph/solo-consensus-node"))
	assert.Equal(t, "us-docker.pkg.dev", RegistryHost("us-docker.pkg.dev/hedera-registry/consensus"))
	assert.Equal(t, "ghcr.io", RegistryHost("oci://ghcr.io/hashgraph/x"))
	assert.Equal(t, "consensus-node", RegistryHost("consensus-node"))
}
