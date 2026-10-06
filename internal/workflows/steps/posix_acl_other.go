// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package steps

import "github.com/joomcode/errorx"

func setDefaultGroupACL(string, int) error {
	return errorx.UnsupportedOperation.New("POSIX ACLs are only supported on linux")
}

func aclUnsupported(error) bool { return true }
