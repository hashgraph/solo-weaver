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
	"github.com/stretchr/testify/require"
	"io"
	"testing"
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
