// SPDX-License-Identifier: Apache-2.0

package sysctl

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

const missingKey = "net.ipv4.solo_weaver_no_such_key"

func TestSet_MissingKeyIsNotFound(t *testing.T) {
	err := Set(missingKey, "1")
	require.Error(t, err)
	require.True(t, errorx.IsNotFound(err))
	require.True(t, errorx.IsOfType(err, errorx.InternalError))
}

func TestSet_PermissionDeniedIsNotNotFound(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Skip("needs a real /proc/sys and a non-root user")
	}

	err := Set("kernel.hostname", "solo-weaver-test")
	require.Error(t, err)
	require.False(t, errorx.IsNotFound(err))
}

func TestApplyConfigs_SkipsMissingKeys(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "15-foreign.conf")
	require.NoError(t, os.WriteFile(conf, []byte(missingKey+" = 8388608\n"), 0o644))

	require.NoError(t, applyConfigs(conf))
}
