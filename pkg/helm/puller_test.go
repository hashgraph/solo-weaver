// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
