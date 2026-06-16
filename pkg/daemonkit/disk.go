// SPDX-License-Identifier: Apache-2.0

package daemonkit

import (
	"context"
	"os"
	"os/user"
	"strconv"
	"syscall"

	"github.com/joomcode/errorx"
)

// DiskPermissionProbe verifies that Path exists and has at least the declared
// permission bits set on the inode. It checks the file mode returned by
// os.Stat — i.e. declared permissions, not actual process-level access.
//
// Use DiskWriteTestProbe when you need to confirm the running process can actually
// write to a directory (takes side effects into account: ownership, ACLs, etc.).
type DiskPermissionProbe struct {
	// Path is the file or directory to inspect.
	Path string

	// Permission is the set of mode bits that must all be present.
	// Examples: 0o400 (owner-read), 0o600 (owner read+write), 0o700 (owner rwx).
	Permission os.FileMode
}

// Probe implements Probe. Returns nil when Path exists and its
// permission bits include all bits in Permission. Returns an error immediately
// on any failure — callers supply their own retry loop if needed.
func (p *DiskPermissionProbe) Probe(_ context.Context) error {
	info, err := os.Stat(p.Path)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "disk probe: cannot access %s", p.Path)
	}
	actual := info.Mode().Perm()
	if actual&p.Permission != p.Permission {
		return errorx.IllegalState.New("disk probe: %s has permissions %04o, need at least %04o",
			p.Path, actual, p.Permission)
	}
	return nil
}

// DiskWriteTestProbe verifies that the running process can actually write to Dir
// by creating and immediately removing a temporary file. Unlike DiskPermissionProbe
// it exercises real process-level access — ownership, ACLs, mount flags, and
// SELinux/AppArmor policies are all tested implicitly.
//
// Use this when the daemon must write to a directory at runtime and you want a
// startup guarantee that the write will succeed (e.g. the upgrade staging dir).
type DiskWriteTestProbe struct {
	// Dir is the directory to test write access in.
	Dir string
}

// Probe implements Probe. Creates a temporary file in Dir and removes it
// immediately. Returns nil on success, an error if the write fails for any reason.
func (p *DiskWriteTestProbe) Probe(_ context.Context) error {
	f, err := os.CreateTemp(p.Dir, ".probe-*")
	if err != nil {
		return errorx.ExternalError.Wrap(err, "disk write-test probe: cannot write to %s", p.Dir)
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return nil
}

// DiskOwnershipProbe verifies that Path exists and matches the declared owner,
// group, and/or permission bits. Any field left at its zero value is skipped:
//   - User == ""        → owner username not checked
//   - Group == ""       → owning group not checked
//   - Permission == 0   → permission bits not checked
//
// Example — ensure /opt/hgcapp is owned by hedera:hedera with rwxr-xr-x:
//
//	&DiskOwnershipProbe{
//	    Path:       "/opt/hgcapp",
//	    User:       "hedera",
//	    Group:      "hedera",
//	    Permission: 0o755,
//	}
//
// Note: ownership is read from the inode via syscall.Stat_t. This probe does
// not check whether the current process has access — use DiskWriteTestProbe for
// that.
type DiskOwnershipProbe struct {
	// Path is the file or directory to inspect.
	Path string

	// User is the expected owner username (e.g. "hedera"). Empty = skip.
	User string

	// Group is the expected owning group name (e.g. "hedera", "weaver"). Empty = skip.
	Group string

	// Permission is the set of mode bits that must all be present (e.g. 0o755).
	// Zero = skip.
	Permission os.FileMode
}

// Probe implements Probe.
func (p *DiskOwnershipProbe) Probe(_ context.Context) error {
	info, err := os.Stat(p.Path)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "disk ownership probe: cannot access %s", p.Path)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errorx.IllegalState.New("disk ownership probe: cannot read inode info for %s", p.Path)
	}

	if p.User != "" {
		u, err := user.Lookup(p.User)
		if err != nil {
			return errorx.ExternalError.Wrap(err, "disk ownership probe: user %q not found", p.User)
		}
		wantUID, err := strconv.ParseUint(u.Uid, 10, 32)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "disk ownership probe: user %q has non-numeric uid %q", p.User, u.Uid)
		}
		if uint32(wantUID) != stat.Uid {
			return errorx.IllegalState.New("disk ownership probe: %s owned by uid %d, want user %q (uid %d)",
				p.Path, stat.Uid, p.User, wantUID)
		}
	}

	if p.Group != "" {
		g, err := user.LookupGroup(p.Group)
		if err != nil {
			return errorx.ExternalError.Wrap(err, "disk ownership probe: group %q not found", p.Group)
		}
		wantGID, err := strconv.ParseUint(g.Gid, 10, 32)
		if err != nil {
			return errorx.IllegalState.Wrap(err, "disk ownership probe: group %q has non-numeric gid %q", p.Group, g.Gid)
		}
		if uint32(wantGID) != stat.Gid {
			return errorx.IllegalState.New("disk ownership probe: %s owned by gid %d, want group %q (gid %d)",
				p.Path, stat.Gid, p.Group, wantGID)
		}
	}

	if p.Permission != 0 {
		actual := info.Mode().Perm()
		if actual&p.Permission != p.Permission {
			return errorx.IllegalState.New("disk ownership probe: %s has permissions %04o, need at least %04o",
				p.Path, actual, p.Permission)
		}
	}

	return nil
}
