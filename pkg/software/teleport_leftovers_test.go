// SPDX-License-Identifier: Apache-2.0

package software

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// teleportHost lays out a fake host: a sandbox bin dir, a system bin dir, the
// agent config and its unit file, all under one temp dir.
type teleportHost struct {
	sandbox, system, config, unit string
	binaries                      []platformBinary
}

var teleportBinaryNames = []string{"tctl", "teleport", "teleport-update", "tsh"}

// newTeleportHost returns a host whose expected binaries are those of the
// version being installed: each file's content is "<name> new".
func newTeleportHost(t *testing.T) *teleportHost {
	t.Helper()
	root := t.TempDir()
	h := &teleportHost{
		sandbox: filepath.Join(root, "sandbox", "bin"),
		system:  filepath.Join(root, "usr", "local", "bin"),
		config:  filepath.Join(root, "etc", "teleport", "teleport.yaml"),
		unit:    filepath.Join(root, "systemd", "teleport.service"),
	}
	for _, dir := range []string{h.sandbox, h.system, filepath.Dir(h.config), filepath.Dir(h.unit)} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	for _, name := range teleportBinaryNames {
		h.binaries = append(h.binaries, platformBinary{
			path:      filepath.Join(h.sandbox, name),
			algorithm: "sha256",
			checksum:  fmt.Sprintf("%x", sha256.Sum256([]byte(name+" new"))),
		})
	}
	return h
}

func (h *teleportHost) writeBinary(t *testing.T, name, release string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(h.sandbox, name), []byte(name+" "+release), 0o755))
}

func (h *teleportHost) links() map[string]string {
	links := map[string]string{}
	for _, name := range teleportBinaryNames {
		links[filepath.Join(h.system, name)] = filepath.Join(h.sandbox, name)
	}
	return links
}

func (h *teleportHost) leftovers(t *testing.T) []string {
	t.Helper()
	got, err := teleportLeftovers(h.binaries, []string{h.config, h.unit}, h.links())
	require.NoError(t, err)
	return got
}

func TestTeleportLeftovers_FreshHostHasNone(t *testing.T) {
	assert.Empty(t, newTeleportHost(t).leftovers(t))
}

// An install of the same version that stopped before configuration is
// finished, not refused.
func TestTeleportLeftovers_UnconfiguredSameVersionBinariesCanBeCompleted(t *testing.T) {
	h := newTeleportHost(t)
	h.writeBinary(t, "teleport", "new")
	h.writeBinary(t, "tctl", "new")

	assert.Empty(t, h.leftovers(t))
}

// A configured agent missing a binary is refused even at the same version:
// install would replace its patched unit file and could not configure it again.
func TestTeleportLeftovers_ConfiguredAgentIsReportedAtAnyVersion(t *testing.T) {
	h := newTeleportHost(t)
	h.writeBinary(t, "teleport", "new")
	h.writeBinary(t, "tctl", "new")
	require.NoError(t, os.WriteFile(h.config, []byte("config"), 0o600))

	assert.Equal(t, []string{h.config}, h.leftovers(t))
}

// An 18.6.4 agent missing a binary must not get the new version's binaries.
func TestTeleportLeftovers_OtherVersionBinariesAreReported(t *testing.T) {
	h := newTeleportHost(t)
	h.writeBinary(t, "teleport", "old")
	h.writeBinary(t, "tctl", "old")
	h.writeBinary(t, "tsh", "new")

	assert.Equal(t, []string{filepath.Join(h.sandbox, "tctl"), filepath.Join(h.sandbox, "teleport")}, h.leftovers(t))
}

// With no binary left, the version of the config, unit and links is unknown.
func TestTeleportLeftovers_DeploymentWithoutBinariesIsReported(t *testing.T) {
	h := newTeleportHost(t)
	require.NoError(t, os.WriteFile(h.config, []byte("config"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join(h.sandbox, "..", "teleport.service"), h.unit))
	require.NoError(t, os.Symlink(filepath.Join(h.sandbox, "teleport"), filepath.Join(h.system, "teleport")))

	assert.ElementsMatch(t, []string{h.config, h.unit, filepath.Join(h.system, "teleport")}, h.leftovers(t))
}

// A command the operator installed themselves is not a weaver agent file.
func TestTeleportLeftovers_ForeignCommandsAreNotEvidence(t *testing.T) {
	h := newTeleportHost(t)
	require.NoError(t, os.WriteFile(filepath.Join(h.system, "tsh"), []byte("operator tsh"), 0o755))
	require.NoError(t, os.Symlink("/opt/teleport/bin/tctl", filepath.Join(h.system, "tctl")))

	assert.Empty(t, h.leftovers(t))
}

func TestTeleportNodeAgentLeftovers_UnknownVersionIsAnError(t *testing.T) {
	_, err := TeleportNodeAgentLeftovers("9.9.9")
	require.Error(t, err)
}

// A configured agent whose binaries are intact but one command link is gone
// reports its configuration, so configure is refused rather than run again.
func TestConfiguredFiles_ConfiguredAgentMissingOneLink(t *testing.T) {
	h := newTeleportHost(t)
	for _, name := range teleportBinaryNames {
		h.writeBinary(t, name, "old")
	}
	require.NoError(t, os.WriteFile(h.config, []byte("config"), 0o600))
	var want []string
	for _, name := range []string{"tctl", "teleport", "teleport-update"} {
		link := filepath.Join(h.system, name)
		require.NoError(t, os.Symlink(filepath.Join(h.sandbox, name), link))
		want = append(want, link)
	}
	want = append(want, h.config)

	got, err := configuredFiles([]string{h.config, h.unit}, h.links())
	require.NoError(t, err)
	assert.ElementsMatch(t, want, got)
}

// Right after a fresh install there are binaries but no configuration yet.
func TestConfiguredFiles_FreshInstallHasNone(t *testing.T) {
	h := newTeleportHost(t)
	for _, name := range teleportBinaryNames {
		h.writeBinary(t, name, "new")
	}

	got, err := configuredFiles([]string{h.config, h.unit}, h.links())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestTeleportNodeAgentConfiguration_UnknownVersionIsAnError(t *testing.T) {
	_, err := TeleportNodeAgentConfiguration("9.9.9")
	require.Error(t, err)
}
