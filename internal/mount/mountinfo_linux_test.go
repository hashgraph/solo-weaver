// SPDX-License-Identifier: Apache-2.0

//go:build linux

package mount

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withProcMountInfoFile points procMountInfoFile at a temp file holding
// content, restoring the previous value afterward.
func withProcMountInfoFile(t *testing.T, content string) {
	t.Helper()
	original := procMountInfoFile
	t.Cleanup(func() { procMountInfoFile = original })

	path := filepath.Join(t.TempDir(), "mountinfo")
	require.NoError(t, os.WriteFile(path, []byte(content), models.DefaultFilePerm))
	procMountInfoFile = path
}

// Test_ClassifyStoragePaths_ReadsAndClassifies exercises the production glue
// this file exists for: open procMountInfoFile, parse it, and classify the
// requested paths against it with the real mountinfo.NetworkTransport prober
// wired in (not a stub). The storage dirs themselves are not created — install
// creates them later — but their parent mount points are real directories so
// path resolution walks up to the intended mount, not past it.
//
// The fake devIDs/sources below don't correspond to any real device on the
// test machine, so NetworkTransport can't resolve a fabric for them; the
// fstype rule alone must be what flags the nfs4 mount non-local.
func Test_ClassifyStoragePaths_ReadsAndClassifies(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	localMount := filepath.Join(root, "blocknode")
	nfsMount := filepath.Join(root, "nfs")
	require.NoError(t, os.Mkdir(localMount, 0o755))
	require.NoError(t, os.Mkdir(nfsMount, 0o755))

	withProcMountInfoFile(t, `27 1 253:0 / / rw,relatime shared:1 - ext4 /dev/vda1 rw,errors=remount-ro
90 27 245:99 / `+localMount+` rw,relatime shared:40 - xfs /dev/zzzfake1 rw
120 27 0:45 / `+nfsMount+` rw,relatime shared:64 - nfs4 10.0.0.5:/export rw,vers=4.2
`)

	findings, err := ClassifyStoragePaths(map[string]string{
		"archive": filepath.Join(localMount, "archive"),
		"log":     filepath.Join(nfsMount, "logs"),
	})
	require.NoError(t, err)
	require.Len(t, findings, 2)

	byMount := map[string]bool{}
	for _, f := range findings {
		byMount[f.MountPoint] = f.Local
	}
	assert.Equal(t, true, byMount[localMount])
	assert.Equal(t, false, byMount[nfsMount])
}

// Test_ClassifyStoragePaths_NoMatchingMount confirms an unmatched path yields
// no findings rather than an error — the check this feeds is warn-only.
func Test_ClassifyStoragePaths_NoMatchingMount(t *testing.T) {
	withProcMountInfoFile(t, `120 27 0:45 / /mnt/nfs-does-not-exist rw,relatime shared:64 - nfs4 10.0.0.5:/export rw,vers=4.2
`)

	findings, err := ClassifyStoragePaths(map[string]string{"archive": "/opt/hedera/blocknode/archive"})
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func Test_ClassifyStoragePaths_FileNotFound(t *testing.T) {
	original := procMountInfoFile
	t.Cleanup(func() { procMountInfoFile = original })
	procMountInfoFile = "/nonexistent/path/to/mountinfo"

	findings, err := ClassifyStoragePaths(map[string]string{"archive": "/data/archive"})
	require.Error(t, err)
	assert.Nil(t, findings)
	assert.Contains(t, err.Error(), "failed to open")
}

func Test_ClassifyStoragePaths_UnparsableContentIsNotFatal(t *testing.T) {
	// mountinfo.Parse skips records it doesn't recognize rather than failing;
	// this wrapper must not turn a garbage file into an error either.
	withProcMountInfoFile(t, "not a mountinfo record\n")

	findings, err := ClassifyStoragePaths(map[string]string{"archive": "/data/archive"})
	require.NoError(t, err)
	assert.Empty(t, findings)
}
