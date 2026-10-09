// SPDX-License-Identifier: Apache-2.0

//go:build linux

package steps

import (
	"encoding/binary"
	"errors"

	"github.com/joomcode/errorx"
	"golang.org/x/sys/unix"
)

const (
	aclXattrDefault = "system.posix_acl_default"
	aclVersion      = 0x0002

	aclUserObj  = 0x01
	aclGroupObj = 0x04
	aclGroup    = 0x08
	aclMask     = 0x10
	aclOther    = 0x20

	aclUndefinedID = 0xFFFFFFFF
)

// setDefaultGroupACL sets a default ACL on dir (u::rwx, g::r-x, g:<gid>:rwx, m::rwx, o::r-x)
// so directories created inside it by other users inherit group rwx for gid regardless of
// the creator's umask.
func setDefaultGroupACL(dir string, gid int) error {
	type entry struct {
		tag, perm uint16
		id        uint32
	}
	entries := []entry{
		{aclUserObj, 7, aclUndefinedID},
		{aclGroupObj, 5, aclUndefinedID},
		{aclGroup, 7, uint32(gid)},
		{aclMask, 7, aclUndefinedID},
		{aclOther, 5, aclUndefinedID},
	}
	buf := make([]byte, 4, 4+8*len(entries))
	binary.LittleEndian.PutUint32(buf, aclVersion)
	for _, e := range entries {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint16(b[0:], e.tag)
		binary.LittleEndian.PutUint16(b[2:], e.perm)
		binary.LittleEndian.PutUint32(b[4:], e.id)
		buf = append(buf, b...)
	}
	if err := unix.Setxattr(dir, aclXattrDefault, buf, 0); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to set default ACL on %s", dir)
	}
	return nil
}

// aclUnsupported reports whether err means the filesystem has no ACL support.
func aclUnsupported(err error) bool {
	return errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.ENOTTY)
}
