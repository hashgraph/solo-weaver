// SPDX-License-Identifier: Apache-2.0

package principal

import "github.com/joomcode/errorx"

type darwinProvider struct{}

func NewProvider() Provider {
	return &darwinProvider{}
}

func (p *darwinProvider) WriteGroupEntry(_ string, _ int) error {
	return errorx.UnsupportedOperation.New("user/group creation not supported on macOS")
}

func (p *darwinProvider) WriteGroupShadowEntry(_ string) error {
	return errorx.UnsupportedOperation.New("user/group creation not supported on macOS")
}

func (p *darwinProvider) WriteUserEntry(_ string, _ int, _ string) error {
	return errorx.UnsupportedOperation.New("user/group creation not supported on macOS")
}

func (p *darwinProvider) WriteUserShadowEntry(_ string) error {
	return errorx.UnsupportedOperation.New("user/group creation not supported on macOS")
}

func (p *darwinProvider) AddMemberToGroup(_ string, _ string) error {
	return errorx.UnsupportedOperation.New("user/group creation not supported on macOS")
}

func (p *darwinProvider) EnumerateUsers(m Manager) ([]User, error) {
	dsclUsers, err := dsclEnumerateUsers()
	if err != nil {
		return nil, err
	}

	users := make([]User, len(dsclUsers))
	for i, u := range dsclUsers {
		u.manager = m
		users[i] = u
	}

	return users, nil
}

func (p *darwinProvider) EnumerateGroups(m Manager) ([]Group, error) {
	dsclGroups, err := dsclEnumerateGroups()
	if err != nil {
		return nil, err
	}

	groups := make([]Group, len(dsclGroups))
	for i, g := range dsclGroups {
		g.manager = m
		groups[i] = g
	}

	return groups, nil
}
