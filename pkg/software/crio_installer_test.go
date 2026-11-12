// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

// Test_generateExpectedCrioSocketDropIn pins the crio.service drop-in that bridges
// cri-o's sandbox API socket to the default /var/run/crio/crio.sock path that
// vendored cAdvisor dials with a hardcoded const (issue #22). Without this bridge
// the "crio-images" imagefs label is never registered and kubelet's eviction
// manager logs `non-existent label "crio-images"` on every sync.
func Test_generateExpectedCrioSocketDropIn(t *testing.T) {
	installer, err := NewCrioInstaller()
	require.NoError(t, err)

	ci, ok := installer.(*crioInstaller)
	require.True(t, ok, "expected *crioInstaller")

	content, err := ci.generateExpectedCrioSocketDropIn()
	require.NoError(t, err)

	sandboxSocket := filepath.Join(models.Paths().SandboxDir, "var/run/crio/crio.sock")

	// systemd override section
	require.Contains(t, content, "[Service]")

	// ExecStartPost recreates the default-path symlink on every start (surviving the
	// tmpfs /run wipe on reboot); it must mkdir the parent and point at the sandbox socket.
	require.Contains(t, content, "ExecStartPost=")
	require.Contains(t, content, "mkdir -p /var/run/crio")
	require.Contains(t, content, "ln -sfn "+sandboxSocket+" /var/run/crio/crio.sock")

	// ExecStopPost removes the symlink; the leading '-' keeps cleanup from failing the unit.
	require.Contains(t, content, "ExecStopPost=-")
	require.Contains(t, content, "rm -f /var/run/crio/crio.sock")

	// systemd native exec form only — the interpolated sandbox path must never be
	// handed to a shell for re-parsing (no "sh -c" exec wrapper).
	require.NotContains(t, content, "sh -c")

	// The drop-in bridges to the sandbox socket; it must never rebind cri-o's listen socket.
	require.NotContains(t, content, "crio.api.listen")
}

// Test_crioSocketDropInPaths pins the sandbox and host locations of the socket-bridge drop-in.
func Test_crioSocketDropInPaths(t *testing.T) {
	require.Equal(t, "10-sandbox-socket.conf", CrioSocketDropInFile)
	require.Equal(t, "crio.service.d", CrioServiceDropInDir)

	require.Equal(t,
		filepath.Join(models.Paths().SandboxDir, "usr", "lib", "systemd", "system", CrioServiceDropInDir),
		getCrioServiceDropInDir())
	require.Equal(t,
		filepath.Join(getCrioServiceDropInDir(), CrioSocketDropInFile),
		getCrioSocketDropInPath())
	require.Equal(t, "/usr/lib/systemd/system/crio.service.d", getSystemCrioServiceDropInDir())
}

func TestRemoveCrioReleaseFiles(t *testing.T) {
	restore := models.SetPaths(t.TempDir())
	defer restore()
	sandbox := models.Paths().SandboxDir

	var release []string
	for _, p := range crioInstalledBinaryPaths(sandbox) {
		release = append(release, p)
	}
	for _, p := range crioReleaseConfigs(sandbox) {
		release = append(release, p)
	}
	ours := []string{
		filepath.Join(sandbox, crioConfdDir, "20-ours.conf"),
		filepath.Join(sandbox, containersRegistriesConfdDir, "mirror.conf"),
	}
	for _, p := range append(append([]string{}, release...), ours...) {
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
	}

	require.NoError(t, RemoveCrioReleaseFiles())

	for _, p := range release {
		assert.NoFileExists(t, p)
	}
	for _, p := range ours {
		assert.FileExists(t, p)
	}
	require.NoError(t, RemoveCrioReleaseFiles(), "a second run finds nothing to remove")
}

func TestRemoveCrioReleaseFiles_FailsOnWhatItCannotRemove(t *testing.T) {
	restore := models.SetPaths(t.TempDir())
	defer restore()
	blocked := crioInstalledBinaryPaths(models.Paths().SandboxDir)["conmon"]
	require.NoError(t, os.MkdirAll(filepath.Join(blocked, "inside"), 0o755))

	err := RemoveCrioReleaseFiles()

	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.IllegalState), err.Error())
	assert.Contains(t, err.Error(), blocked)
}
