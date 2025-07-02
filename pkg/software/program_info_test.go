/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
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

package software

import (
	"crypto/sha256"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProgramInfo_Getters(t *testing.T) {
	req := require.New(t)
	p := programInfo{
		path:       "/path",
		mode:       0777,
		version:    "0.0.1",
		sha256Hash: fmt.Sprintf("%x", sha256.Sum256([]byte("test"))),
	}

	req.Equal(p.mode, p.GetFileMode())
	req.Equal(p.sha256Hash, p.GetHash())
	req.Equal(p.path, p.GetPath())
	req.Equal(p.version, p.GetVersion())
}

func TestProgramInfo_IsExecAll(t *testing.T) {
	req := require.New(t)
	p := programInfo{
		path:       "/path",
		mode:       0111,
		version:    "0.0.1",
		sha256Hash: fmt.Sprintf("%x", sha256.Sum256([]byte("test"))),
	}
	req.True(p.IsExecAll())

	p = programInfo{
		path:       "/path",
		mode:       0110,
		version:    "0.0.1",
		sha256Hash: fmt.Sprintf("%x", sha256.Sum256([]byte("test"))),
	}
	req.False(p.IsExecAll())
}

func TestProgramInfo_IsExecAny(t *testing.T) {
	req := require.New(t)
	p := programInfo{
		path:       "/path",
		mode:       0144,
		version:    "0.0.1",
		sha256Hash: fmt.Sprintf("%x", sha256.Sum256([]byte("test"))),
	}
	req.True(p.IsExecAny())

	p = programInfo{
		path:       "/path",
		mode:       0444,
		version:    "0.0.1",
		sha256Hash: fmt.Sprintf("%x", sha256.Sum256([]byte("test"))),
	}
	req.False(p.IsExecAny())
}

func TestProgramInfo_IsExecGroup(t *testing.T) {
	req := require.New(t)
	p := programInfo{
		path:       "/path",
		mode:       0010,
		version:    "0.0.1",
		sha256Hash: fmt.Sprintf("%x", sha256.Sum256([]byte("test"))),
	}
	req.True(p.IsExecGroup())

	p = programInfo{
		path:       "/path",
		mode:       0444,
		version:    "0.0.1",
		sha256Hash: fmt.Sprintf("%x", sha256.Sum256([]byte("test"))),
	}
	req.False(p.IsExecGroup())
}

func TestProgramInfo_IsExecOwner(t *testing.T) {
	req := require.New(t)
	p := programInfo{
		path:       "/path",
		mode:       0100,
		version:    "0.0.1",
		sha256Hash: fmt.Sprintf("%x", sha256.Sum256([]byte("test"))),
	}
	req.True(p.IsExecOwner())

	p = programInfo{
		path:       "/path",
		mode:       0444,
		version:    "0.0.1",
		sha256Hash: fmt.Sprintf("%x", sha256.Sum256([]byte("test"))),
	}
	req.False(p.IsExecOwner())
}
