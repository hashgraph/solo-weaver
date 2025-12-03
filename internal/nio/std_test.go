// SPDX-License-Identifier: Apache-2.0

package nio

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTestIOStreams(t *testing.T) {
	req := require.New(t)

	streams, in, err, out := NewTestIOStreams()
	req.NotNil(in)
	req.NotNil(out)
	req.NotNil(err)
	req.Equal(in, streams.In)
	req.Equal(out, streams.Out)
	req.Equal(err, streams.ErrOut)
}

func TestNewTestIOStreamsDiscard(t *testing.T) {
	req := require.New(t)

	streams := NewTestIOStreamsDiscard()
	req.NotNil(streams.In)
	req.NotNil(streams.Out)
	req.NotNil(streams.ErrOut)
	req.Equal(io.Discard, streams.Out)
	req.Equal(io.Discard, streams.ErrOut)
}
