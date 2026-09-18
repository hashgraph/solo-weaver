// SPDX-License-Identifier: Apache-2.0

package mountinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadFixture(t *testing.T, name string) []Entry {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	entries, err := Parse(f)
	require.NoError(t, err)
	return entries
}

// withResolver replaces the path resolver for the duration of a test so
// Classify can be exercised against paths that do not exist on this machine.
func withResolver(t *testing.T, fn func(string) string) {
	t.Helper()
	prev := resolvePath
	resolvePath = fn
	t.Cleanup(func() { resolvePath = prev })
}

func Test_Parse_SplitsOnSeparatorNotOffset(t *testing.T) {
	entries := loadFixture(t, "mountinfo_sample.txt")

	byMount := map[string]Entry{}
	for _, e := range entries {
		byMount[e.MountPoint] = e
	}

	// Zero optional fields: the separator sits immediately after the options.
	assert.Equal(t, "overlay", byMount["/mnt/overlay"].FSType)
	// One optional field.
	assert.Equal(t, "nfs4", byMount["/mnt/nfs"].FSType)
	assert.Equal(t, "10.0.0.5:/export", byMount["/mnt/nfs"].Source)
	// Three optional fields — a fixed offset would read "propagate_from:3".
	assert.Equal(t, "xfs", byMount["/mnt/data"].FSType)
	assert.Equal(t, "/dev/sdc1", byMount["/mnt/data"].Source)
	// A bind mount reports the fstype of the backing device, not "none".
	assert.Equal(t, "ext4", byMount["/mnt/bind"].FSType)
	// Octal escapes in the mount point are decoded.
	assert.Equal(t, "xfs", byMount["/mnt/with space"].FSType)
	// The maj:min of field 3 is kept so the transport probe can look the device
	// up in sysfs without stat'ing the source.
	assert.Equal(t, "8:33", byMount["/mnt/data"].DevID)
	assert.Equal(t, "0:46", byMount["/mnt/s3"].DevID)
	// The unparsable line is skipped, not fatal.
	assert.NotContains(t, byMount, "is")
}

func Test_Parse_EmptyInput(t *testing.T) {
	entries, err := Parse(strings.NewReader(""))
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func Test_Find_LongestPrefixWins(t *testing.T) {
	entries := loadFixture(t, "mountinfo_sample.txt")

	e, ok := Find(entries, "/mnt/s3/archive/2024")
	require.True(t, ok)
	assert.Equal(t, "/mnt/s3", e.MountPoint)
	assert.Equal(t, "fuse.s3fs", e.FSType)

	// A path with no dedicated mount falls back to "/".
	e, ok = Find(entries, "/opt/hedera/blocknode")
	require.True(t, ok)
	assert.Equal(t, "/", e.MountPoint)
	assert.Equal(t, "ext4", e.FSType)

	// "/mnt/nfs-backup" must not match the "/mnt/nfs" mount point.
	e, ok = Find(entries, "/mnt/nfs-backup")
	require.True(t, ok)
	assert.Equal(t, "/", e.MountPoint)
}

func Test_Find_LastMountAtSamePointWins(t *testing.T) {
	entries := []Entry{
		{MountPoint: "/data", FSType: "ext4", Source: "/dev/sda1"},
		{MountPoint: "/data", FSType: "tmpfs", Source: "tmpfs"},
	}
	e, ok := Find(entries, "/data/live")
	require.True(t, ok)
	assert.Equal(t, "tmpfs", e.FSType)
}

func Test_Find_NoEntries(t *testing.T) {
	_, ok := Find(nil, "/data")
	assert.False(t, ok)
}

func Test_IsLocal(t *testing.T) {
	tests := []struct {
		name  string
		entry Entry
		want  bool
	}{
		{"ext4 on sata", Entry{FSType: "ext4", Source: "/dev/sda1"}, true},
		{"xfs on nvme", Entry{FSType: "xfs", Source: "/dev/nvme0n1p1"}, true},
		{"zfs pool", Entry{FSType: "zfs", Source: "tank/data"}, true},
		{"nfs", Entry{FSType: "nfs4", Source: "10.0.0.5:/export"}, false},
		{"fuse object store", Entry{FSType: "fuse.s3fs", Source: "s3fs"}, false},
		{"tmpfs", Entry{FSType: "tmpfs", Source: "tmpfs"}, false},
		{"overlay", Entry{FSType: "overlay", Source: "overlay"}, false},
		{"ceph rbd carrying ext4", Entry{FSType: "ext4", Source: "/dev/rbd0"}, false},
		{"nbd carrying ext4", Entry{FSType: "ext4", Source: "/dev/nbd3"}, false},
		{"drbd carrying xfs", Entry{FSType: "xfs", Source: "/dev/drbd1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsLocal(tt.entry))
		})
	}
}

func Test_ResolveForLookup_NonExistentPathWalksToAncestor(t *testing.T) {
	root := t.TempDir()
	realRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)

	// Storage dirs are created later in install; the path may not exist yet.
	got := ResolveForLookup(filepath.Join(root, "archive", "not", "created", "yet"))
	assert.Equal(t, realRoot, got)
}

func Test_ResolveForLookup_FollowsSymlink(t *testing.T) {
	root := t.TempDir()
	realRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)

	target := filepath.Join(realRoot, "real-data")
	require.NoError(t, os.MkdirAll(target, 0o755))
	link := filepath.Join(realRoot, "data-link")
	require.NoError(t, os.Symlink(target, link))

	assert.Equal(t, target, ResolveForLookup(link))
	assert.Equal(t, target, ResolveForLookup(filepath.Join(link, "..", "real-data")))
}

func Test_Classify_GroupsVolumesSharingAMount(t *testing.T) {
	withResolver(t, func(p string) string { return p })
	entries := loadFixture(t, "all_local.txt")

	paths := map[string]string{
		"archive":           "/opt/hedera/blocknode/archive",
		"live":              "/opt/hedera/blocknode/live",
		"log":               "/opt/hedera/blocknode/logs",
		"plugins":           "/opt/hedera/blocknode/plugins",
		"application-state": "/opt/hedera/blocknode/application-state",
	}

	findings := Classify(paths, entries, nil)
	require.Len(t, findings, 1, "five paths under one mount must report once, not five times")
	assert.Equal(t, "/opt/hedera/blocknode", findings[0].MountPoint)
	assert.Equal(t, "xfs", findings[0].FSType)
	assert.True(t, findings[0].Local)
	assert.Equal(t,
		[]string{"application-state", "archive", "live", "log", "plugins"},
		findings[0].Volumes)
}

func Test_Classify_SplitsAcrossMountsAndFlagsNonLocal(t *testing.T) {
	withResolver(t, func(p string) string { return p })
	entries := loadFixture(t, "mountinfo_sample.txt")

	paths := map[string]string{
		"archive": "/mnt/s3/archive",
		"live":    "/mnt/data/live",
		"log":     "/mnt/ceph/logs",
	}

	findings := Classify(paths, entries, nil)
	require.Len(t, findings, 3)

	byMount := map[string]Finding{}
	for _, f := range findings {
		byMount[f.MountPoint] = f
	}
	assert.False(t, byMount["/mnt/s3"].Local)
	assert.Equal(t, "fuse.s3fs", byMount["/mnt/s3"].FSType)
	assert.True(t, byMount["/mnt/data"].Local)
	// ext4 on an rbd device is network-attached despite a local fstype.
	assert.False(t, byMount["/mnt/ceph"].Local)
}

func Test_Classify_SkipsEmptyAndUnresolvablePaths(t *testing.T) {
	withResolver(t, func(p string) string { return p })

	// No root mount to match; a warn-only check reports nothing, not non-local.
	entries := []Entry{{MountPoint: "/mnt/other", FSType: "ext4", Source: "/dev/sda1"}}
	findings := Classify(map[string]string{"archive": "/data/archive", "live": "  "}, entries, nil)
	assert.Empty(t, findings)
}

func Test_Classify_ProbeFlagsNetworkTransport(t *testing.T) {
	withResolver(t, func(p string) string { return p })

	// An iSCSI LUN is ext4 on a plain sd* device: nothing in the mountinfo
	// record distinguishes it from local SATA, so only the probe can.
	entries := []Entry{{DevID: "8:17", MountPoint: "/mnt/data", FSType: "ext4", Source: "/dev/sdb1"}}
	probe := func(devID, source string) string {
		assert.Equal(t, "8:17", devID)
		assert.Equal(t, "/dev/sdb1", source)
		return "iSCSI"
	}

	findings := Classify(map[string]string{"archive": "/mnt/data/archive"}, entries, probe)
	require.Len(t, findings, 1)
	assert.False(t, findings[0].Local)
	assert.Equal(t, "iSCSI", findings[0].Transport)
}

func Test_Classify_ProbeNotConsultedForAlreadyNonLocalMounts(t *testing.T) {
	withResolver(t, func(p string) string { return p })

	entries := []Entry{
		{DevID: "0:46", MountPoint: "/mnt/s3", FSType: "fuse.s3fs", Source: "s3fs"},
		{DevID: "252:16", MountPoint: "/mnt/ceph", FSType: "ext4", Source: "/dev/rbd0"},
	}
	probe := func(string, string) string {
		t.Fatal("probe must not run for a mount the fstype and device-name rules already rejected")
		return ""
	}

	findings := Classify(map[string]string{"archive": "/mnt/s3/a", "live": "/mnt/ceph/l"}, entries, probe)
	require.Len(t, findings, 2)
	for _, f := range findings {
		assert.False(t, f.Local)
		assert.Empty(t, f.Transport, "the fstype alone already says why these are not local")
	}
}

func Test_Classify_ProbeReportingLocalLeavesFindingLocal(t *testing.T) {
	withResolver(t, func(p string) string { return p })

	entries := []Entry{{DevID: "8:1", MountPoint: "/mnt/data", FSType: "ext4", Source: "/dev/sda1"}}
	findings := Classify(map[string]string{"live": "/mnt/data/live"}, entries, func(string, string) string { return "" })
	require.Len(t, findings, 1)
	assert.True(t, findings[0].Local)
	assert.Empty(t, findings[0].Transport)
}

func Test_Summarize_NamesTheTransportCarryingALocalFilesystem(t *testing.T) {
	findings := []Finding{
		{MountPoint: "/mnt/data", FSType: "ext4", Volumes: []string{"archive"}, Transport: "iSCSI"},
	}
	s := Summarize(findings)

	// "is on ext4, not local block storage" would read as a contradiction.
	assert.Contains(t, s.Warning, "/mnt/data (archive) is on ext4 over iSCSI, not local block storage")
	assert.Equal(t, []string{"archive: ext4 over iSCSI on /mnt/data (not local block storage)"}, s.Media)
	assert.Equal(t, "iSCSI", s.Metadata["storage.archive.transport"])
	assert.Equal(t, "ext4", s.Metadata["storage.archive.fstype"])
}

func Test_Summarize_AlwaysRecordsMediaPerVolume(t *testing.T) {
	findings := []Finding{
		{MountPoint: "/opt/hedera/blocknode", FSType: "xfs", Volumes: []string{"archive", "live"}, Local: true},
	}
	s := Summarize(findings)

	assert.Empty(t, s.Warning)
	assert.Equal(t, map[string]string{
		"storage.archive.fstype":     "xfs",
		"storage.archive.mountpoint": "/opt/hedera/blocknode",
		"storage.live.fstype":        "xfs",
		"storage.live.mountpoint":    "/opt/hedera/blocknode",
	}, s.Metadata, "a local mount records no transport key")
	assert.Equal(t, []string{
		"archive: xfs on /opt/hedera/blocknode",
		"live: xfs on /opt/hedera/blocknode",
	}, s.Media, "media lines are rendered even when nothing is wrong")
}

func Test_Summarize_WarnsPerNonLocalMount(t *testing.T) {
	findings := []Finding{
		{MountPoint: "/mnt/data", FSType: "xfs", Volumes: []string{"live"}, Local: true},
		{MountPoint: "/mnt/s3", FSType: "fuse.s3fs", Volumes: []string{"archive", "log"}, Local: false},
	}
	s := Summarize(findings)

	require.NotEmpty(t, s.Warning)
	assert.Equal(t, 1, strings.Count(s.Warning, "\n")+1, "one warning line per non-local mount")
	assert.Contains(t, s.Warning, "/mnt/s3 (archive, log) is on fuse.s3fs")
	assert.Contains(t, s.Warning, "liveness timeout")
	assert.NotContains(t, s.Warning, "/mnt/data")
	assert.Equal(t, "xfs", s.Metadata["storage.live.fstype"])
	// One line per volume, in findings order, and only the non-local ones
	// carry the suffix.
	assert.Equal(t, []string{
		"live: xfs on /mnt/data",
		"archive: fuse.s3fs on /mnt/s3 (not local block storage)",
		"log: fuse.s3fs on /mnt/s3 (not local block storage)",
	}, s.Media)
}

func Test_Summarize_NoFindings(t *testing.T) {
	s := Summarize(nil)
	assert.Empty(t, s.Warning)
	assert.Empty(t, s.Media)
	assert.Empty(t, s.Metadata)
	assert.NotNil(t, s.Metadata)
}
