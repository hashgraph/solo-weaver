//go:build !darwin && !windows && !plan9

package principal

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestUnixProvider_WithRoot_EnumeratedUsers(t *testing.T) {
	manager, err := NewManager()
	assert.NoError(t, err)
	provider := NewProvider()
	userList, eErr := provider.EnumerateUsers(manager)
	assert.NoError(t, eErr)
	assert.NotEmpty(t, userList)
	foundRootUser := false
	for _, user := range userList {
		if user.Name() == "root" {
			foundRootUser = true
		}
	}
	assert.True(t, foundRootUser)
}

func TestUnixProvider_WithRoot_EnumeratedGroups(t *testing.T) {
	manager, err := NewManager()
	assert.NoError(t, err)
	provider := NewProvider()
	groupList, eErr := provider.EnumerateGroups(manager)
	assert.NoError(t, eErr)
	assert.NotEmpty(t, groupList)
	foundDefaultGroup := false
	for _, group := range groupList {
		if group.Name() == "nobody" {
			foundDefaultGroup = true
		}
		if group.Name() == "nogroup" {
			foundDefaultGroup = true
		}
	}
	assert.True(t, foundDefaultGroup)
}
