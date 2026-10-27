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
	// The operator's effective values win, so bn-health follows the BN's real port.
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

func TestResolvePublisherPort(t *testing.T) {
	tests := []struct {
		name   string
		values string
		want   string
	}{
		{name: "dedicated publisher port wins", values: "service:\n  port: 40840\nblockNode:\n  ports:\n    publisher: 40984\n", want: "40984"},
		{name: "string value supported", values: "blockNode:\n  ports:\n    publisher: \"40984\"\n", want: "40984"},
		{name: "null publisher shares service.port", values: "service:\n  port: 41840\nblockNode:\n  ports:\n    publisher: null\n", want: "41840"},
		{name: "neither set falls back to the chart default", values: "blockNode:\n  ports:\n    health: 40983\n", want: DefaultBlockNodeServicePort},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ResolvePublisherPort(writeValues(t, tc.values))
			require.NoError(t, err)
			require.Equal(t, tc.want, p)
		})
	}
}

func TestResolvePublisherPort_NoFileReturnsDefault(t *testing.T) {
	p, err := ResolvePublisherPort("")
	require.NoError(t, err)
	require.Equal(t, DefaultBlockNodeServicePort, p)
}

func TestResolvePublisherPort_MissingFileErrors(t *testing.T) {
	_, err := ResolvePublisherPort(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	require.Error(t, err)
}
