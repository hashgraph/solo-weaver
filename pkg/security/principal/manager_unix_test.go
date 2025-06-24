package principal

import (
	assertions "github.com/stretchr/testify/assert"
	"os/user"
	"runtime"
	"testing"
)

func TestDefaultManager_WithProvider_UserExistsByName(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	dm := &defaultManager{provider: NewProvider()}
	u, err := user.Current()
	assert.NoError(err)

	assert.True(dm.UserExistsByName(u.Username))
	assert.True(dm.UserExistsByName("root"))
	assert.False(dm.UserExistsByName("xyz"))
}

func TestDefaultManager_WithProvider_GroupExistsByName(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	dm := &defaultManager{provider: NewProvider()}
	u, err := user.Current()
	assert.NoError(err)
	g, err := user.LookupGroupId(u.Gid)
	assert.NoError(err)

	assert.True(dm.GroupExistsByName(g.Name))
	if runtime.GOOS == "darwin" {
		assert.True(dm.GroupExistsByName("staff"))
	} else {
		assert.True(dm.GroupExistsByName("root"))
	}
	assert.False(dm.GroupExistsByName("xyz"))
}

func TestDefaultManager_WithProvider_LookupUserById(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	dm := &defaultManager{provider: NewProvider()}
	u, err := user.Current()
	assert.NoError(err)

	foundUser, err := dm.LookupUserById(u.Uid)
	if assert.NoError(err) {
		assert.Equal(u.Username, foundUser.Name())
		assert.GreaterOrEqual(len(foundUser.Groups()), 0)
		assert.NotNil(foundUser.PrimaryGroup())
	}

	foundUser, err = dm.LookupUserById("0")
	if assert.NoError(err) {

		assert.Equal("root", foundUser.Name())
		assert.GreaterOrEqual(len(foundUser.Groups()), 0)
		assert.NotNil(foundUser.PrimaryGroup())
	}

	notFoundGroup, err := dm.LookupGroupById("999999")
	assert.Error(err)
	assert.Nil(notFoundGroup)
}

func TestDefaultManager_WithProvider_LookupGroupById(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	dm := &defaultManager{provider: NewProvider()}
	u, err := user.Current()
	assert.NoError(err)
	g, err := user.LookupGroupId(u.Gid)
	assert.NoError(err)

	foundGroup, err := dm.LookupGroupById(g.Gid)
	if assert.NoError(err) {
		assert.Equal(g.Name, foundGroup.Name())
		assert.GreaterOrEqual(len(foundGroup.Users()), 0)
	}

	foundGroup, err = dm.LookupGroupById("0")
	if assert.NoError(err) {
		if runtime.GOOS == "darwin" {
			assert.Equal("wheel", foundGroup.Name())
		} else {
			assert.Equal("root", foundGroup.Name())
		}

		if runtime.GOOS == "darwin" {
			assert.Greater(len(foundGroup.Users()), 0)
		} else {
			assert.Equal(0, len(foundGroup.Users()))
		}
	}

	notFoundGroup, err := dm.LookupGroupById("999999")
	assert.Error(err)
	assert.Nil(notFoundGroup)
}
