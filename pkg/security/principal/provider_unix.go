// SPDX-License-Identifier: Apache-2.0

//go:build !darwin && !windows

package principal

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joomcode/errorx"
)

const (
	unixPasswordFile    = "/etc/passwd"
	unixGroupFile       = "/etc/group"
	unixShadowFile      = "/etc/shadow"
	unixGroupShadowFile = "/etc/gshadow"
	unixNologinShell    = "/usr/sbin/nologin"
)

type unixProvider struct{}

func NewProvider() Provider {
	return &unixProvider{}
}

func (p *unixProvider) EnumerateUsers(m Manager) ([]User, error) {
	entities, err := readEntityFile(unixPasswordFile, parseUnixUser)
	if err != nil {
		return nil, err
	}

	users := make([]User, len(entities))
	for i, e := range entities {
		e.manager = m
		users[i] = e
	}

	return users, nil
}

func (p *unixProvider) WriteGroupEntry(name string, gid int) error {
	return appendToFile(unixGroupFile, fmt.Sprintf("%s:x:%d:\n", name, gid))
}

func (p *unixProvider) WriteUserEntry(name string, uid int, homeDir string) error {
	// System user: no gecos, nologin shell.
	// Primary GID equals UID; caller must create the group first.
	return appendToFile(unixPasswordFile, fmt.Sprintf("%s:x:%d:%d::%s:%s\n", name, uid, uid, homeDir, unixNologinShell))
}

func (p *unixProvider) WriteGroupShadowEntry(name string) error {
	return appendToFileIfExists(unixGroupShadowFile, fmt.Sprintf("%s:!::\n", name))
}

func (p *unixProvider) WriteUserShadowEntry(name string) error {
	days := int(time.Now().Unix() / 86400)
	return appendToFileIfExists(unixShadowFile, fmt.Sprintf("%s:!:%d:0:99999:7:::\n", name, days))
}

func (p *unixProvider) AddMemberToGroup(groupName, memberName string) error {
	// Open read-only: writes go to a sibling temp file and are renamed in atomically.
	f, err := os.OpenFile(unixGroupFile, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	found := false
	for i, line := range lines {
		parts := strings.SplitN(line, ":", 4)
		if len(parts) != 4 || parts[0] != groupName {
			continue
		}
		found = true
		members := strings.TrimSpace(parts[3])
		if members != "" {
			for _, m := range strings.Split(members, ",") {
				if strings.TrimSpace(m) == memberName {
					return nil
				}
			}
			parts[3] = members + "," + memberName
		} else {
			parts[3] = memberName
		}
		lines[i] = strings.Join(parts, ":")
		break
	}
	if !found {
		return errorx.IllegalState.New("group %q not found in %s", groupName, unixGroupFile)
	}

	// Preserve the original file's permissions on the replacement.
	stat, err := f.Stat()
	if err != nil {
		return err
	}

	// Write to a sibling temp file and rename atomically so a crash mid-write
	// cannot leave /etc/group in a partially-written state.
	tmp, err := os.CreateTemp(filepath.Dir(unixGroupFile), ".group.tmp.*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		_ = tmp.Close()
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	for _, line := range lines {
		if _, err := fmt.Fprintln(tmp, line); err != nil {
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, stat.Mode().Perm()); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, unixGroupFile); err != nil {
		return err
	}
	success = true
	return nil
}

func (p *unixProvider) EnumerateGroups(m Manager) ([]Group, error) {
	entities, err := readEntityFile(unixGroupFile, parseUnixGroup)
	if err != nil {
		return nil, err
	}

	groups := make([]Group, len(entities))
	for i, e := range entities {
		e.manager = m
		groups[i] = e
	}

	return groups, nil
}
