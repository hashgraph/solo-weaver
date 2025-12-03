// SPDX-License-Identifier: Apache-2.0

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
