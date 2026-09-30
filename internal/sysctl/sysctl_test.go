// SPDX-License-Identifier: Apache-2.0

package sysctl

import (
	"errors"
	"os"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

func Test_isMissingSysctl(t *testing.T) {
	missing := errorx.InternalError.Wrap(os.ErrNotExist, "failed to set /proc/sys/net/ipv4/udp_rmem_max")
	require.True(t, isMissingSysctl(missing))

	denied := errorx.InternalError.Wrap(os.ErrPermission, "failed to set /proc/sys/net/ipv4/tcp_rmem")
	require.False(t, isMissingSysctl(denied))

	require.False(t, isMissingSysctl(errors.New("write failed")))
}
