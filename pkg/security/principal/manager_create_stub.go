// SPDX-License-Identifier: Apache-2.0

//go:build darwin || windows

package principal

import "github.com/joomcode/errorx"

func (m *defaultManager) CreateGroup(_ string) (Group, error) {
	return nil, errorx.UnsupportedOperation.New("group creation not supported on this platform")
}

func (m *defaultManager) CreateGroupWithId(_ string, _ int) (Group, error) {
	return nil, errorx.UnsupportedOperation.New("group creation not supported on this platform")
}

func (m *defaultManager) CreateUser(_ string) (User, error) {
	return nil, errorx.UnsupportedOperation.New("user creation not supported on this platform")
}

func (m *defaultManager) CreateUserWithId(_ string, _ int, _ string) (User, error) {
	return nil, errorx.UnsupportedOperation.New("user creation not supported on this platform")
}

func (m *defaultManager) AddUserToGroup(_, _ string) error {
	return errorx.UnsupportedOperation.New("group membership modification not supported on this platform")
}
