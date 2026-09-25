// SPDX-License-Identifier: Apache-2.0

// Package mountinfo classifies filesystem paths by the storage media backing them.
// The parsing and classification rules carry no build tags so they stay testable
// everywhere; only the sysfs transport probe (transport_linux.go) is Linux-only.
package mountinfo

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joomcode/errorx"
)

// Entry is the subset of a /proc/self/mountinfo record this package needs.
type Entry struct {
	// DevID is the "maj:min" of the backing device. tmpfs, nfs and btrfs
	// report a synthetic number here.
	DevID string
	// MountPoint is the mount point relative to the process root.
	MountPoint string
	// FSType is the filesystem type, e.g. "ext4", "nfs4", "fuse.s3fs".
	FSType string
	// Source is the mount source, e.g. "/dev/sda1", "10.0.0.1:/export".
	Source string
}

// localFSTypes allowlists filesystems backed by a local block device.
// An allowlist, not a denylist of NFS/FUSE, so an unlisted future driver
// defaults to non-local instead of silently passing.
var localFSTypes = map[string]struct{}{
	"btrfs": {},
	"ext2":  {},
	"ext3":  {},
	"ext4":  {},
	"f2fs":  {},
	"xfs":   {},
	"zfs":   {},
}

// networkDevicePrefixes are network block device names. iSCSI, Fibre Channel
// and NVMe-oF LUNs look like plain sd*/nvme* devices; a DeviceProber catches those.
var networkDevicePrefixes = []string{"rbd", "nbd", "drbd"}

// DeviceProber reports the network fabric carrying a block device, or "" when it
// is local or unresolvable. Only consulted for mounts the other rules accept.
type DeviceProber func(devID, source string) string

// Parse reads /proc/self/mountinfo content. Records it doesn't recognise are
// skipped rather than failing the whole read, since this feeds a warn-only check.
func Parse(r io.Reader) ([]Entry, error) {
	var entries []Entry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		e, ok := parseLine(sc.Text())
		if !ok {
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to read mountinfo")
	}
	return entries, nil
}

// parseLine splits one mountinfo record. The fstype and source come right
// after the variable-length optional-fields section, marked by a "-".
func parseLine(line string) (Entry, bool) {
	fields := strings.Fields(line)
	// 6 fixed fields precede the optional section, and the separator is
	// followed by at least the fstype and the mount source.
	if len(fields) < 10 {
		return Entry{}, false
	}
	sep := -1
	for i := 6; i < len(fields); i++ {
		if fields[i] == "-" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+2 >= len(fields) {
		return Entry{}, false
	}
	return Entry{
		DevID:      fields[2],
		MountPoint: unescape(fields[4]),
		FSType:     fields[sep+1],
		Source:     unescape(fields[sep+2]),
	}, true
}

// unescape decodes the octal escapes the kernel applies to space, tab, newline
// and backslash in mountinfo path fields.
func unescape(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var v int
			if _, err := fmt.Sscanf(s[i+1:i+4], "%03o", &v); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Find returns the mount backing p (already resolved via ResolveForLookup) by
// longest-prefix match. When mounts share a mount point, the last one wins,
// matching kernel overmount semantics.
func Find(entries []Entry, p string) (Entry, bool) {
	var best Entry
	bestLen := -1
	for _, e := range entries {
		if !under(p, e.MountPoint) {
			continue
		}
		if len(e.MountPoint) >= bestLen {
			best, bestLen = e, len(e.MountPoint)
		}
	}
	return best, bestLen >= 0
}

// under reports whether p is the mount point mp or lies beneath it.
func under(p, mp string) bool {
	if mp == "/" {
		return true
	}
	return p == mp || strings.HasPrefix(p, mp+"/")
}

// IsLocal reports whether e is backed by a local block device.
func IsLocal(e Entry) bool {
	if _, ok := localFSTypes[e.FSType]; !ok {
		return false
	}
	dev := filepath.Base(e.Source)
	for _, prefix := range networkDevicePrefixes {
		if strings.HasPrefix(dev, prefix) {
			return false
		}
	}
	return true
}

// ResolveForLookup returns the nearest existing ancestor of p with symlinks
// resolved. Storage dirs are created later in install, so p may not exist yet;
// a symlinked data dir must still resolve to the mount actually backing it.
func ResolveForLookup(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	for cur := abs; ; {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return resolved
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		cur = parent
	}
}

// resolvePath is a var so tests can exercise Classify's grouping against
// synthetic paths that don't exist on the test machine (same pattern as
// internal/mount/mount_unix.go).
var resolvePath = ResolveForLookup

// Finding is one mount point backing one or more block-node storage volumes.
type Finding struct {
	MountPoint string
	FSType     string
	// Volumes are the block-node volume names (archive, live, log, …) that
	// land on this mount, sorted.
	Volumes []string
	// Transport names the network fabric, e.g. "iSCSI". Empty for a local device
	// and for a mount already rejected on fstype or device name.
	Transport string
	Local     bool
}

// media renders the filesystem, plus the fabric under it when the filesystem
// alone would read as local.
func (f Finding) media() string {
	if f.Transport == "" {
		return f.FSType
	}
	return f.FSType + " over " + f.Transport
}

// Classify maps volume name -> storage path onto the mounts backing them,
// grouping volumes that share a mount point. Unmatched paths are omitted. probe may be nil.
func Classify(paths map[string]string, entries []Entry, probe DeviceProber) []Finding {
	names := make([]string, 0, len(paths))
	for n := range paths {
		names = append(names, n)
	}
	sort.Strings(names)

	byMount := make(map[string]*Finding, len(names))
	var order []string
	for _, n := range names {
		p := strings.TrimSpace(paths[n])
		if p == "" {
			continue
		}
		e, ok := Find(entries, resolvePath(p))
		if !ok {
			continue
		}
		f, seen := byMount[e.MountPoint]
		if !seen {
			// A device already known to be remote needs no sysfs lookup, and its
			// fstype names the problem better than a fabric would.
			transport := ""
			local := IsLocal(e)
			if local && probe != nil {
				transport = probe(e.DevID, e.Source)
				local = transport == ""
			}
			f = &Finding{MountPoint: e.MountPoint, FSType: e.FSType, Transport: transport, Local: local}
			byMount[e.MountPoint] = f
			order = append(order, e.MountPoint)
		}
		f.Volumes = append(f.Volumes, n)
	}

	sort.Strings(order)
	findings := make([]Finding, 0, len(order))
	for _, mp := range order {
		findings = append(findings, *byMount[mp])
	}
	return findings
}

// Summary is the report-ready view of a set of findings.
type Summary struct {
	// Metadata always has one fstype and one mountpoint entry per volume, so
	// the saved workflow report records backing media even when nothing is wrong.
	Metadata map[string]string
	// Warning is empty when every volume is on local block storage.
	Warning string
	// Media is one console line per volume, "<volume>: <fstype> on <mountpoint>",
	// in findings order.
	Media []string
}

// Summarize renders findings into step-report metadata, the per-volume media
// lines shown on the console, and an operator-facing warning.
func Summarize(findings []Finding) Summary {
	s := Summary{Metadata: make(map[string]string)}
	var warnings []string
	for _, f := range findings {
		for _, v := range f.Volumes {
			s.Metadata["storage."+v+".fstype"] = f.FSType
			s.Metadata["storage."+v+".mountpoint"] = f.MountPoint
			if f.Transport != "" {
				s.Metadata["storage."+v+".transport"] = f.Transport
			}

			line := fmt.Sprintf("%s: %s on %s", v, f.media(), f.MountPoint)
			if !f.Local {
				line += " (not local block storage)"
			}
			s.Media = append(s.Media, line)
		}
		if !f.Local {
			warnings = append(warnings, fmt.Sprintf(
				"%s (%s) is on %s, not local block storage; the block node scans every stored block at startup and may exceed its liveness timeout",
				f.MountPoint, strings.Join(f.Volumes, ", "), f.media()))
		}
	}
	s.Warning = strings.Join(warnings, "\n")
	return s
}
