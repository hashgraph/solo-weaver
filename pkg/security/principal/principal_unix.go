package principal

import (
	"fmt"
	erx "github.com/joomcode/errorx"
	"runtime"
	"strconv"
	"strings"
)

// unixUser is a unix implementation of the User interface.
type unixUser struct {
	// manager is the Manager instance which created this object.
	manager Manager
	// name is the user's login name.
	name string
	// displayName is the user's display name.
	displayName string
	// uid is the user's unique identifier.
	uid int
	// gid is the user's primary group identifier.
	gid int
	// homeDir is the absolute path to the user's home directory.
	homeDir string
	// shell is the user's command interpreter.
	shell string
	// primaryGroup is the user's primary group as resolved during Manager.Refresh.
	primaryGroup Group
	// groups are the user's extended list of group membership as resolved during Manager.Refresh.
	groups []Group
}

func (u *unixUser) Uid() string {
	return strconv.Itoa(int(u.uid))
}

func (u *unixUser) Name() string {
	return u.name
}

func (u *unixUser) DisplayName() string {
	return u.displayName
}

func (u *unixUser) HomeDir() string {
	return u.homeDir
}

func (u *unixUser) Shell() string {
	return u.shell
}

func (u *unixUser) PrimaryGroup() Group {
	return u.primaryGroup
}

func (u *unixUser) Groups() []Group {
	return u.groups
}

func (u *unixUser) Validate() error {
	if len(strings.TrimSpace(u.name)) == 0 {
		return erx.IllegalArgument.New("User name cannot be empty: %s", u.Name)
	}

	if u.uid == 0 && strings.TrimSpace(u.name) != "root" {
		return erx.IllegalArgument.New("UID cannot be zero (0) for a non-root user: %s", u.gid)
	}

	if u.gid == 0 && strings.TrimSpace(u.name) != "root" {
		return erx.IllegalArgument.New("GID cannot be zero (0) for a non-root user: %s", u.gid)
	}

	if len(strings.TrimSpace(u.homeDir)) == 0 {
		u.homeDir = fmt.Sprintf("/home/%s", u.name)
	}

	if len(strings.TrimSpace(u.shell)) == 0 {
		if runtime.GOOS == "darwin" {
			u.shell = "/bin/zsh"
		} else {
			u.shell = "/bin/bash"
		}
	}

	return nil
}

// unixGroup is a unix implementation of the Group interface.
type unixGroup struct {
	// manager is the Manager instance which created this object.
	manager Manager
	// name is the group's name.
	name string
	// gid is the group's unique identifier.
	gid int
	// members is the list of group member usernames as defined in /etc/group or via dscl.
	members []string
	// users are the group's list of users as resolved during Manager.Refresh.
	users []User
}

func (g *unixGroup) Gid() string {
	return strconv.Itoa(int(g.gid))
}

func (g *unixGroup) Name() string {
	return g.name
}

func (g *unixGroup) Users() []User {
	return g.users
}

func (g *unixGroup) Validate() error {
	if len(strings.TrimSpace(g.name)) == 0 {
		return erx.IllegalArgument.New("Group name cannot be empty: %s", g.name)
	}

	if g.gid == 0 && strings.TrimSpace(g.name) != "root" {
		return erx.IllegalArgument.New("GID cannot be zero (0) for a non-root group: %s", g.gid)
	}

	return nil
}
