// SPDX-License-Identifier: Apache-2.0

// Package principal provides routines to set and check security for users, groups, files, and folders.
//
// Use NewManager() to get an operating system specific implementation.
package principal

// Manager provides routines to set and check security for users, groups, files, and folders.
//
// The Manager interface is used with the NewManager() method to allow for multiple implementations
// based on the operating system.  The current implementation is to support Linux as Linux machines, VMs,
// and Docker containers are our primary focus at this time.
type Manager interface {
	// UserExistsByName provided the username returns true if it exists else false.
	UserExistsByName(userName string) bool
	// GroupExistsByName provided the group name returns true if it exists else false.
	GroupExistsByName(groupName string) bool
	// CreateUser creates a user with the given username.  The UID will be automatically generated.
	CreateUser(userName string) (User, error)
	// CreateUserWithId creates a user with the given username and predefined UID.
	// On windows, the uid parameter is ignored; therefore, this method would be synonymous with the CreateUser method.
	CreateUserWithId(userName string, uid int) (User, error)
	// CreateGroup creates a group with the given group name.  The GID will be automatically generated.
	CreateGroup(groupName string) (Group, error)
	// CreateGroupWithId creates a group with the given group name and predefined GID.
	// On windows, the gid parameter is ignored; therefore, this method would be synonymous with the CreateGroup method.
	CreateGroupWithId(groupName string, gid int) (Group, error)
	// LookupUserByName provided the username returns the user object or an error. If the user does not exist, an error is returned.
	LookupUserByName(userName string) (User, error)
	// LookupUserById provided the user id returns the user object or an error. If the user does not exist, an error is returned.
	LookupUserById(uid string) (User, error)
	// LookupGroupByName provided the group name returns the group object or an error. If the group does not exist, an error is returned.
	LookupGroupByName(groupName string) (Group, error)
	// LookupGroupById provided the group id returns the group object or an error. If the group does not exist, an error is returned.
	LookupGroupById(gid string) (Group, error)
	// Refresh refreshes the user and group cache.
	Refresh() error
}

// Provider is an abstraction for user and group principal operations which provides the environment specific logic.
// The default implementation uses the operating system's user and group database.
// All Provider implementations must be thread safe.
type Provider interface {
	// EnumerateUsers queries the underlying operating system registry for all users.
	EnumerateUsers(m Manager) ([]User, error)
	// EnumerateGroups queries the underlying operating system registry for all groups.
	EnumerateGroups(m Manager) ([]Group, error)
}

// User is an operating system agnostic representation of a local or directory service connected user principal.
type User interface {
	// Id returns the user id.
	Uid() string
	// Name returns the username. This is the name that the user logs in with.
	Name() string
	// DisplayName returns the user's display name. On windows, this is the user's full name.
	DisplayName() string
	// HomeDir returns the user's home directory.
	HomeDir() string
	// PrimaryGroup returns the user's primary group.
	PrimaryGroup() Group
	// Groups returns the user's groups.
	Groups() []Group
	// Validate returns an error if the user is not valid.
	Validate() error
}

// Group is an operating system agnostic representation of a local or directory service connected group principal.
type Group interface {
	// Id returns the group id.
	Gid() string
	// Name returns the group name.
	Name() string
	// Users returns the users that are members of this group.
	Users() []User
	// Validate returns an error if the group is not valid.
	Validate() error
}
