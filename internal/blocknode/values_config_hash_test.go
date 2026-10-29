// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func parseValues(t *testing.T, b []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	require.NoError(t, yaml.Unmarshal(b, &m))
	return m
}

// configHash is deterministic and change-sensitive: the same content hashes the
// same, different content hashes differently.
func TestConfigHash_Deterministic(t *testing.T) {
	a := configHash([]byte("blockNode:\n  config:\n    X: \"1\"\n"))
	again := configHash([]byte("blockNode:\n  config:\n    X: \"1\"\n"))
	diff := configHash([]byte("blockNode:\n  config:\n    X: \"2\"\n"))

	assert.Equal(t, a, again, "identical values must hash identically (idempotent reconfigure)")
	assert.NotEqual(t, a, diff, "changed values must hash differently (rolls the pod)")
	assert.Len(t, a, 64, "sha256 hex digest is 64 chars")
}

// SetConfigHash adds the annotation under podAnnotations using the operator
// namespace, and an empty hash removes it (and drops an emptied podAnnotations map).
func TestSetConfigHash_SetAndRemove(t *testing.T) {
	out, err := SetConfigHash([]byte("service:\n  type: LoadBalancer\n"), "deadbeef")
	require.NoError(t, err)
	vals := parseValues(t, out)
	ann, ok := vals["podAnnotations"].(map[string]interface{})
	require.True(t, ok, "podAnnotations must be present")
	assert.Equal(t, "deadbeef", ann[ConfigHashAnnotation])
	assert.Equal(t, "operator.solo.hiero.org/config-hash", ConfigHashAnnotation)

	// Empty hash removes the annotation; with nothing else under podAnnotations the
	// map is dropped so the pod template matches a never-hashed deployment.
	cleared, err := SetConfigHash(out, "")
	require.NoError(t, err)
	assert.NotContains(t, parseValues(t, cleared), "podAnnotations")
}

// SetConfigHash preserves other operator-supplied pod annotations.
func TestSetConfigHash_PreservesOtherAnnotations(t *testing.T) {
	in := []byte("podAnnotations:\n  example.com/keep: \"yes\"\n")
	out, err := SetConfigHash(in, "abc123")
	require.NoError(t, err)

	ann := parseValues(t, out)["podAnnotations"].(map[string]interface{})
	assert.Equal(t, "yes", ann["example.com/keep"], "existing annotation must be preserved")
	assert.Equal(t, "abc123", ann[ConfigHashAnnotation])

	// Removing the config hash leaves the other annotation intact.
	cleared, err := SetConfigHash(out, "")
	require.NoError(t, err)
	ann = parseValues(t, cleared)["podAnnotations"].(map[string]interface{})
	assert.Equal(t, "yes", ann["example.com/keep"])
	assert.NotContains(t, ann, ConfigHashAnnotation)
}
