// SPDX-License-Identifier: Apache-2.0

package labels

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testProfile is a minimal Profiler implementation for testing.
type testProfile struct {
	name string
}

func (t testProfile) Name() string { return t.name }
func (t testProfile) Labels(input LabelInput) map[string]string {
	return map[string]string{"profile": t.name, "cluster": input.ClusterName}
}

func TestRegisterAndIsValid(t *testing.T) {
	Register(testProfile{name: "alpha"})

	assert.True(t, IsValid("alpha"))
	assert.True(t, IsValid("Alpha"), "should be case-insensitive")
	assert.False(t, IsValid(""))
	assert.False(t, IsValid("beta"))
}

func TestValidNames(t *testing.T) {
	Register(testProfile{name: "zebra"})
	Register(testProfile{name: "alpha"})

	names := ValidNames()
	// Registry includes eng, ops (from init) plus test profiles alpha, zebra
	require.GreaterOrEqual(t, len(names), 2)
	assert.Contains(t, names, "alpha")
	assert.Contains(t, names, "zebra")
	// Verify sorted order
	for i := 1; i < len(names); i++ {
		assert.Less(t, names[i-1], names[i], "names should be sorted alphabetically")
	}
}

func TestResolve(t *testing.T) {
	Register(testProfile{name: "ops"})

	t.Run("empty profile uses default profile", func(t *testing.T) {
		assert.NotNil(t, Resolve("", LabelInput{ClusterName: "cluster1", DeployProfile: "prod"}))
	})

	t.Run("unknown profile returns nil", func(t *testing.T) {
		assert.Nil(t, Resolve("unknown", LabelInput{ClusterName: "cluster1", DeployProfile: "prod"}))
	})

	t.Run("known profile delegates to Labels()", func(t *testing.T) {
		result := Resolve("ops", LabelInput{ClusterName: "my-cluster", DeployProfile: "prod"})
		assert.Equal(t, map[string]string{"profile": "ops", "cluster": "my-cluster"}, result)
	})

	t.Run("case-insensitive lookup", func(t *testing.T) {
		result := Resolve("Ops", LabelInput{ClusterName: "my-cluster", DeployProfile: "prod"})
		assert.NotNil(t, result)
	})
}
