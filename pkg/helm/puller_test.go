// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullAndVerify_UnsupportedAlgorithm(t *testing.T) {
	hm := newTestHelmManager(t)
	_, err := hm.PullAndVerify(context.Background(), t.TempDir(), "metallb/metallb", "0.15.2", "md5", "deadbeef")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported checksum algorithm")
	assert.Contains(t, err.Error(), "md5")
	assert.Contains(t, err.Error(), "sha256")
}

func TestPullAndVerify_EmptyChecksum(t *testing.T) {
	hm := newTestHelmManager(t)
	_, err := hm.PullAndVerify(context.Background(), t.TempDir(), "metallb/metallb", "0.15.2", "sha256", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected checksum is empty")
}

func TestPullAndVerify_EmptyDestDir(t *testing.T) {
	hm := newTestHelmManager(t)
	_, err := hm.PullAndVerify(context.Background(), "", "metallb/metallb", "0.15.2", "sha256", "deadbeef")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "destDir is empty")
}

func Test_sha256File(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "data.bin")
	payload := []byte("the quick brown fox jumps over the lazy dog\n")
	require.NoError(t, os.WriteFile(tmp, payload, 0o600))

	expected := sha256.Sum256(payload)
	got, err := sha256File(tmp)
	require.NoError(t, err)
	assert.Equal(t, hex.EncodeToString(expected[:]), got)
}

func TestIsTransientChartFetchError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "http 400", err: fmt.Errorf("http 400 response"), want: false},
		{name: "gateway timeout", err: fmt.Errorf("failed to fetch https://example.com: 504 Gateway Timeout"), want: true},
		{name: "bad gateway", err: fmt.Errorf("502 Bad Gateway"), want: true},
		{name: "service unavailable", err: fmt.Errorf("503 Service Unavailable"), want: true},
		{name: "timeout", err: fmt.Errorf("request timed out"), want: true},
		{name: "connection reset", err: fmt.Errorf("connection reset by peer"), want: true},
		{name: "non transient", err: fmt.Errorf("chart not found"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isTransientChartFetchError(tc.err))
		})
	}
}

func TestWithChartPullAttempts(t *testing.T) {
	hm := &helmManager{}
	WithChartPullAttempts(5)(hm)
	require.Equal(t, 5, hm.chartPullAttempts)

	WithChartPullAttempts(0)(hm)
	require.Equal(t, 5, hm.chartPullAttempts)

	m, err := NewManager(WithChartPullAttempts(8))
	require.NoError(t, err)
	require.Equal(t, 8, m.(*helmManager).chartPullAttempts)
}

// newTestHelmManager builds a helmManager wired to a temp HELM_* environment
// so tests do not mutate the developer's global helm config.
func newTestHelmManager(t *testing.T) *helmManager {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HELM_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("HELM_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("HELM_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("HELM_REPOSITORY_CONFIG", filepath.Join(home, "config", "repositories.yaml"))
	t.Setenv("HELM_REPOSITORY_CACHE", filepath.Join(home, "cache", "repository"))
	return &helmManager{}
}
