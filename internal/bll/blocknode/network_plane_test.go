// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package blocknode

import (
	"errors"
	"testing"

	bnpkg "github.com/hashgraph/solo-weaver/internal/blocknode"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

func releaseWithPublisherPort(port float64) releaseValuesReader {
	return func(models.BlockNodeInputs) (map[string]any, error) {
		return map[string]any{"blockNode": map[string]any{"ports": map[string]any{"publisher": port}}}, nil
	}
}

func TestResolveBlockNodePorts_ReuseValuesReadsRelease(t *testing.T) {
	ports, err := resolveBlockNodePorts(models.BlockNodeInputs{ReuseValues: true}, releaseWithPublisherPort(40850))
	require.NoError(t, err)
	require.Equal(t, "40850", ports.Publisher)
	require.Equal(t, bnpkg.DefaultBlockNodeHealthPort, ports.Health)
}

func TestResolveBlockNodePorts_NoReuseValuesSkipsRelease(t *testing.T) {
	read := func(models.BlockNodeInputs) (map[string]any, error) {
		t.Fatal("release values read without --reuse-values")
		return nil, nil
	}
	ports, err := resolveBlockNodePorts(models.BlockNodeInputs{ReuseValues: false}, read)
	require.NoError(t, err)
	require.Equal(t, bnpkg.DefaultBlockNodeServicePort, ports.Publisher)
}

func TestResolveBlockNodePorts_NilReaderSkipsRelease(t *testing.T) {
	ports, err := resolveBlockNodePorts(models.BlockNodeInputs{ReuseValues: true}, nil)
	require.NoError(t, err)
	require.Equal(t, bnpkg.DefaultBlockNodeServicePort, ports.Publisher)
}

func TestResolveBlockNodePorts_ReleaseReadErrorFails(t *testing.T) {
	read := func(models.BlockNodeInputs) (map[string]any, error) {
		return nil, errors.New("helm unreachable")
	}
	_, err := resolveBlockNodePorts(models.BlockNodeInputs{ReuseValues: true}, read)
	require.Error(t, err)
}
