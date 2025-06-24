package principal

import (
	"fmt"
	"sync/atomic"
)

// NewManager will detect the operating system and return a Manager implementation to use.
// This will attempt to initialize the user and group cache, returning any errors.
func NewManager() (Manager, error) {
	manager := &defaultManager{provider: NewProvider()}
	// Refresh the user and group cache, ignoring any errors.
	err := manager.Refresh()
	if err != nil {
		return nil, err
	}

	return manager, nil
}

// defaultManager is the primary Manager implementation used by most system components.
// It is backed by an operating system specific Provider implementation.
type defaultManager struct {
	// provider is the underlying operating system layer to use for all operations.
	provider Provider
	// userCache is a cache of User instances, this is used to avoid repeated calls to the provider. The username is used as the key.
	userCache atomic.Pointer[map[string]User]
	// groupCache is a cache of Group instances, this is used to avoid repeated calls to the provider. The group name is used as the key.
	groupCache atomic.Pointer[map[string]Group]
}

// UserExistsByName will return true if the user specified by the userName argument exists; otherwise, false is returned.
func (m *defaultManager) UserExistsByName(userName string) bool {
	_, err := m.LookupUserByName(userName)
	return err == nil
}

// GroupExistsByName will return true if the group specified by the groupName argument exists; otherwise, false is returned.
func (m *defaultManager) GroupExistsByName(groupName string) bool {
	_, err := m.LookupGroupByName(groupName)
	return err == nil
}

func (m *defaultManager) CreateUser(userName string) (User, error) {
	//TODO implement me
	panic("implement me")
}

func (m *defaultManager) CreateUserWithId(userName string, uid int) (User, error) {
	//TODO implement me
	panic("implement me")
}

func (m *defaultManager) CreateGroup(groupName string) (Group, error) {
	//TODO implement me
	panic("implement me")
}

func (m *defaultManager) CreateGroupWithId(groupName string, gid int) (Group, error) {
	//TODO implement me
	panic("implement me")
}

func (m *defaultManager) LookupUserByName(userName string) (User, error) {
	if !m.initialized() {
		err := m.Refresh()
		if err != nil {
			return nil, NewUserNotFoundError(err, userName, "")
		}
	}

	users := m.userCache.Load()
	if user, ok := (*users)[userName]; ok {
		return user, nil
	}

	return nil, NewUserNotFoundError(nil, userName, "")
}

func (m *defaultManager) LookupUserById(uid string) (User, error) {
	if !m.initialized() {
		err := m.Refresh()
		if err != nil {
			return nil, NewUserNotFoundError(err, "", uid)
		}
	}

	users := m.userCache.Load()
	for _, user := range *users {
		if user.Uid() == uid {
			return user, nil
		}
	}

	return nil, NewUserNotFoundError(nil, "", uid)
}

func (m *defaultManager) LookupGroupByName(groupName string) (Group, error) {
	if !m.initialized() {
		err := m.Refresh()
		if err != nil {
			return nil, NewGroupNotFoundError(err, groupName, "")
		}
	}

	groups := m.groupCache.Load()
	if group, ok := (*groups)[groupName]; ok {
		return group, nil
	}

	return nil, NewGroupNotFoundError(nil, groupName, "")
}

func (m *defaultManager) LookupGroupById(gid string) (Group, error) {
	if !m.initialized() {
		err := m.Refresh()
		if err != nil {
			return nil, NewGroupNotFoundError(err, "", gid)
		}
	}

	groups := m.groupCache.Load()

	for _, group := range *groups {
		if group.Gid() == gid {
			return group, nil
		}
	}

	return nil, NewGroupNotFoundError(nil, "", gid)
}

// Refresh will evict all existing user and group cache entries and re-populate the cache.
func (m *defaultManager) Refresh() error {
	if m.provider == nil {
		return fmt.Errorf("the supplied provider is nil but is required, cannot refresh the user and group cache")
	}

	// Clear all the caches and reset the initialized flag.
	m.evictCache()

	userMap, err := m.loadUsers()
	if err != nil {
		return err
	}

	groupMap, err := m.loadGroups()
	if err != nil {
		return err
	}

	if !m.userCache.CompareAndSwap(nil, &userMap) {
		// TODO: log this
	}

	if !m.groupCache.CompareAndSwap(nil, &groupMap) {
		// TODO: log this
	}

	m.processGroupMembership()
	return nil
}

func (m *defaultManager) initialized() bool {
	return m.userCache.Load() != nil && m.groupCache.Load() != nil
}

func (m *defaultManager) evictCache() {
	m.userCache.Store(nil)
	m.groupCache.Store(nil)
}

func (m *defaultManager) loadUsers() (map[string]User, error) {
	if cache := m.userCache.Load(); cache != nil {
		return *cache, nil
	}

	users, err := m.provider.EnumerateUsers(m)
	if err != nil {
		return nil, err
	}

	userMap := make(map[string]User, len(users))
	for _, user := range users {
		userMap[user.Name()] = user
	}

	return userMap, nil
}

func (m *defaultManager) loadGroups() (map[string]Group, error) {
	if cache := m.groupCache.Load(); cache != nil {
		return *cache, nil
	}

	groups, err := m.provider.EnumerateGroups(m)
	if err != nil {
		return nil, err
	}

	groupMap := make(map[string]Group, len(groups))
	for _, group := range groups {
		groupMap[group.Name()] = group
	}

	return groupMap, nil
}
