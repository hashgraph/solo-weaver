// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClusterChecker_ClusterState_ClusterAbsent(t *testing.T) {
	probe := func() (bool, error) { return false, nil }
	c := newClusterChecker(probe)

	st, err := c.ClusterState(context.Background())
	require.NoError(t, err)
	assert.False(t, st.Created, "ClusterState.Created should be false when cluster is absent")
}

func TestClusterChecker_ClusterState_ProbeError(t *testing.T) {
	probe := func() (bool, error) { return false, errors.New("connection refused") }
	c := newClusterChecker(probe)

	st, err := c.ClusterState(context.Background())
	require.NoError(t, err) // error is logged, not propagated
	assert.False(t, st.Created)
}
