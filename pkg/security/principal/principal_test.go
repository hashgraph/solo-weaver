// SPDX-License-Identifier: Apache-2.0

package principal

import (
	"os/user"
)

type mockUser struct {
	user *user.User
}

func (m *mockUser) Uid() string {
	return m.user.Uid
}

func (m *mockUser) Name() string {
	return m.user.Username
}

func (m *mockUser) DisplayName() string {
	return m.user.Name
}

func (m *mockUser) HomeDir() string {
	return m.user.HomeDir
}

func (m *mockUser) PrimaryGroup() Group {
	//TODO implement me
	panic("implement me")
}

func (m *mockUser) Groups() []Group {
	//TODO implement me
	panic("implement me")
}

func (m *mockUser) Validate() error {
	//TODO implement me
	panic("implement me")
}

type mockGroup struct {
	group *user.Group
}

func (m *mockGroup) Gid() string {
	return m.group.Gid
}

func (m *mockGroup) Name() string {
	return m.group.Name
}

func (m *mockGroup) Users() []User {
	//TODO implement me
	panic("implement me")
}

func (m *mockGroup) Validate() error {
	//TODO implement me
	panic("implement me")
}

type mockProvider struct{}

func (ms *mockProvider) EnumerateUsers(m Manager) ([]User, error) {
	return []User{
		&mockUser{
			user: &user.User{
				Uid:      "1000",
				Name:     "validUser",
				Username: "validUser",
			},
		},
	}, nil
}

func (ms *mockProvider) EnumerateGroups(m Manager) ([]Group, error) {
	return []Group{
		&mockGroup{
			group: &user.Group{
				Gid:  "1000",
				Name: "validGroup",
			},
		},
	}, nil
}
