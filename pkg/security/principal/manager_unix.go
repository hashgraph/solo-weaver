// SPDX-License-Identifier: Apache-2.0

package principal

func (m *defaultManager) processGroupMembership() {
	userCache := m.userCache.Load()
	groupCache := m.groupCache.Load()
	groupByIdCache := make(map[int]Group, len(*groupCache))

	for _, group := range *groupCache {
		if unixGroup, ok := group.(*unixGroup); ok {
			groupByIdCache[unixGroup.gid] = unixGroup
			for _, member := range unixGroup.members {
				if user, ok := (*userCache)[member]; ok {
					if unixUser, ok := user.(*unixUser); ok {
						unixGroup.users = append(unixGroup.users, unixUser)
						unixUser.groups = append(unixUser.groups, unixGroup)
					}
				}
			}
		}
	}

	for _, user := range *userCache {
		if unixUser, ok := user.(*unixUser); ok {
			if primaryGroup, ok := groupByIdCache[unixUser.gid]; ok {
				unixUser.primaryGroup = primaryGroup
			}
		}
	}
}
