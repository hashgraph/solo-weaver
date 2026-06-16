// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package daemonkit

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tempSockPath returns a short unix socket path that fits within the
// platform limit (~104 chars on macOS, 108 on Linux). t.TempDir() appends
// the full test function name and can exceed the limit on macOS.
func tempSockPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "n.sock")
}

func Test_NotifyReady_NoopWhenSocketUnset(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	err := NotifyReady()
	assert.NoError(t, err)
}

func Test_NotifyReady_WritesToSocket(t *testing.T) {
	sockPath := tempSockPath(t)

	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sockPath, Net: "unixgram"})
	require.NoError(t, err)
	defer conn.Close()

	t.Setenv("NOTIFY_SOCKET", sockPath)

	err = NotifyReady()
	require.NoError(t, err)

	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, sdReady, string(buf[:n]))
}

func Test_NotifyStopping_WritesPayload(t *testing.T) {
	sockPath := tempSockPath(t)

	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sockPath, Net: "unixgram"})
	require.NoError(t, err)
	defer conn.Close()

	t.Setenv("NOTIFY_SOCKET", sockPath)

	err = NotifyStopping()
	require.NoError(t, err)

	buf := make([]byte, 64)
	n, readErr := conn.Read(buf)
	require.NoError(t, readErr)
	assert.Equal(t, sdStopping, string(buf[:n]))
}

// Verify that notify is a no-op when env var is empty string (not just unset).
func Test_NotifyReady_NoopWhenSocketEmpty(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	err := NotifyReady()
	assert.NoError(t, err)
}
