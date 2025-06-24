/*
 * Copyright 2016-2022 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

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
