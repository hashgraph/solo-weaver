// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeValues(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "values.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	return p
}

func TestResolveHealthPort_NoFileReturnsDefault(t *testing.T) {
	p, err := ResolveHealthPort("")
	require.NoError(t, err)
	require.Equal(t, DefaultBlockNodeHealthPort, p)
}

func TestResolveHealthPort_ReadsOperatorOverride(t *testing.T) {
	// The operator's effective values win, so bn-mgmt follows the BN's real port.
	f := writeValues(t, "blockNode:\n  ports:\n    health: 41000\n")
	p, err := ResolveHealthPort(f)
	require.NoError(t, err)
	require.Equal(t, "41000", p)
}

func TestResolveHealthPort_StringValueSupported(t *testing.T) {
	f := writeValues(t, "blockNode:\n  ports:\n    health: \"41234\"\n")
	p, err := ResolveHealthPort(f)
	require.NoError(t, err)
	require.Equal(t, "41234", p)
}

func TestResolveHealthPort_MissingKeyFallsBackToDefault(t *testing.T) {
	// SERVER_PORT is deliberately NOT consulted (it is the main gRPC port, not the
	// health/statusz port), so this falls through to the chart default.
	f := writeValues(t, "blockNode:\n  config:\n    SERVER_PORT: \"40840\"\n")
	p, err := ResolveHealthPort(f)
	require.NoError(t, err)
	require.Equal(t, DefaultBlockNodeHealthPort, p)
}

func TestResolveHealthPort_MissingFileErrors(t *testing.T) {
	_, err := ResolveHealthPort(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	require.Error(t, err)
}
