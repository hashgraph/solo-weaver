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

package exit

import "github.com/stretchr/testify/require"
import "testing"

func TestCode_Int(t *testing.T) {
	req := require.New(t)

	req.Equal(0, NormalTermination.Int())
	req.Equal(65, DataFormatError.Int())
	req.NotEqual(9999, NormalTermination.Int())
}

func TestCode_String(t *testing.T) {
	req := require.New(t)
	req.Equal("0", NormalTermination.String())
	req.Equal("77", PermissionDenied.String())
	req.NotEqual("65", TemporaryFailure.String())
}

func TestCode_Is(t *testing.T) {
	req := require.New(t)

	req.True(NormalTermination.Is(0))
	req.False(NormalTermination.Is(9999))
	req.True(FileCreationError.Is(73))
	req.False(ServiceUnavailable.Is(70))
}
