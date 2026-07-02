// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsSetNotExistError(t *testing.T) {
	require.True(t, isSetNotExistError("/dev/stdin:1:18-23: Error: No such file or directory\nlist set inet weaver bn-publisher\n"))
	require.False(t, isSetNotExistError("Error: Operation not permitted"))
	require.False(t, isSetNotExistError(""))
}
