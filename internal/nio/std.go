// SPDX-License-Identifier: Apache-2.0

package nio

import (
	"bytes"
	"io"
)

// StdStreams provides the standard names for io-streams.  This is useful for embedding and for unit testing.
type StdStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

// NewTestIOStreams returns a valid StdStreams and in, out, errOut buffers for unit tests
func NewTestIOStreams() (StdStreams, *bytes.Buffer, *bytes.Buffer, *bytes.Buffer) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	return StdStreams{
		In:     in,
		Out:    out,
		ErrOut: errOut,
	}, in, out, errOut
}

// NewTestIOStreamsDiscard returns a valid StdStreams that just discards
func NewTestIOStreamsDiscard() StdStreams {
	in := &bytes.Buffer{}
	return StdStreams{
		In:     in,
		Out:    io.Discard,
		ErrOut: io.Discard,
	}
}
