//go:build darwin

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

package detect

import (
	"github.com/stretchr/testify/require"
	"runtime"
	"testing"
)

func TestDarwinOSDetector_ScanOS(t *testing.T) {
	req := require.New(t)
	dd := NewDarwinOSDetector()
	osInfo, err := dd.ScanOS()
	req.NoError(err)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
	req.NotEmpty(osInfo.Version)
	req.NotEmpty(osInfo.Flavor)
	req.NotEmpty(osInfo.CodeName)
}
