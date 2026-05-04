// SPDX-License-Identifier: Apache-2.0

//go:build !darwin && !windows

package principal

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/joomcode/errorx"
)

const (
	// sysIDMin / sysIDMax define the inclusive range for auto-allocated system
	// IDs when no explicit UID/GID is provided. These match the SYS_UID_MIN /
	// SYS_GID_MIN defaults on Debian, Ubuntu, and RHEL derivatives.
	sysIDMin = 100
	sysIDMax = 999
)

func (m *defaultManager) CreateGroupWithId(groupName string, gid int) (Group, error) {
	if existing, err := m.LookupGroupByName(groupName); err == nil {
		return nil, errorx.IllegalState.New("group %q already exists with GID %s", groupName, existing.Gid())
	}
	if existing, err := m.LookupGroupById(strconv.Itoa(gid)); err == nil {
		return nil, errorx.IllegalState.New("GID %d is already used by group %q", gid, existing.Name())
	}
	if err := m.provider.WriteGroupEntry(groupName, gid); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create group %q", groupName)
	}
	_ = m.provider.WriteGroupShadowEntry(groupName)
	if err := m.Refresh(); err != nil {
		return nil, err
	}
	return m.LookupGroupByName(groupName)
}

func (m *defaultManager) AddUserToGroup(userName, groupName string) error {
	if !m.UserExistsByName(userName) {
		return errorx.IllegalState.New("user %q not found", userName)
	}
	grp, err := m.LookupGroupByName(groupName)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "group %q not found", groupName)
	}
	for _, u := range grp.Users() {
		if u.Name() == userName {
			return nil
		}
	}
	if err := m.provider.AddMemberToGroup(groupName, userName); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to add %q to group %q", userName, groupName)
	}
	return m.Refresh()
}

// CreateGroup allocates the lowest free GID in [sysIDMin, sysIDMax] by
// scanning /etc/group under an exclusive flock, then appends the new entry.
func (m *defaultManager) CreateGroup(groupName string) (Group, error) {
	if _, err := allocateAndWriteGroupEntry(groupName); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create group %q", groupName)
	}
	_ = m.provider.WriteGroupShadowEntry(groupName)
	if err := m.Refresh(); err != nil {
		return nil, err
	}
	return m.LookupGroupByName(groupName)
}

func (m *defaultManager) CreateUserWithId(userName string, uid int, homeDir string) (User, error) {
	if existing, err := m.LookupUserByName(userName); err == nil {
		return nil, errorx.IllegalState.New("user %q already exists with UID %s", userName, existing.Uid())
	}
	if existing, err := m.LookupUserById(strconv.Itoa(uid)); err == nil {
		return nil, errorx.IllegalState.New("UID %d is already used by user %q", uid, existing.Name())
	}
	if err := m.provider.WriteUserEntry(userName, uid, homeDir); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create user %q", userName)
	}
	_ = m.provider.WriteUserShadowEntry(userName)
	if err := m.Refresh(); err != nil {
		return nil, err
	}
	return m.LookupUserByName(userName)
}

// CreateUser allocates the lowest free UID in [sysIDMin, sysIDMax] by
// scanning /etc/passwd under an exclusive flock, then appends the new entry.
// The primary GID is set equal to the allocated UID; the caller must create
// a group with that GID before calling this method.
func (m *defaultManager) CreateUser(userName string) (User, error) {
	if _, err := allocateAndWriteUserEntry(userName); err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create user %q", userName)
	}
	_ = m.provider.WriteUserShadowEntry(userName)
	if err := m.Refresh(); err != nil {
		return nil, err
	}
	return m.LookupUserByName(userName)
}

// allocateAndWriteGroupEntry opens /etc/group with an exclusive flock, scans
// all existing GIDs to find the lowest free one in [sysIDMin, sysIDMax],
// appends the new group entry, and returns the allocated GID.
func allocateAndWriteGroupEntry(name string) (int, error) {
	f, err := os.OpenFile(unixGroupFile, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return 0, err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	gid, err := nextFreeID(f)
	if err != nil {
		return 0, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return 0, err
	}
	if _, err := fmt.Fprintf(f, "%s:x:%d:\n", name, gid); err != nil {
		return 0, err
	}
	return gid, nil
}

// allocateAndWriteUserEntry opens /etc/passwd with an exclusive flock, scans
// all existing UIDs to find the lowest free one in [sysIDMin, sysIDMax],
// appends the new passwd entry (primary GID == UID), and returns the UID.
func allocateAndWriteUserEntry(name string) (int, error) {
	f, err := os.OpenFile(unixPasswordFile, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return 0, err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	uid, err := nextFreeID(f)
	if err != nil {
		return 0, err
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return 0, err
	}
	if _, err := fmt.Fprintf(f, "%s:x:%d:%d::/:%s\n", name, uid, uid, unixNologinShell); err != nil {
		return 0, err
	}
	return uid, nil
}

// nextFreeID scans f (a colon-delimited identity file already held under an
// exclusive flock) and returns the lowest integer in [sysIDMin, sysIDMax]
// that does not appear as the third field (index 2) of any existing line.
func nextFreeID(f *os.File) (int, error) {
	used := make(map[int]bool, 64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), ":", 4)
		if len(parts) >= 3 {
			if id, err := strconv.Atoi(parts[2]); err == nil {
				used[id] = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	for id := sysIDMin; id <= sysIDMax; id++ {
		if !used[id] {
			return id, nil
		}
	}
	return 0, errorx.IllegalState.New("no free system ID in range [%d, %d]", sysIDMin, sysIDMax)
}
