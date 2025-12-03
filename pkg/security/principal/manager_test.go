// SPDX-License-Identifier: Apache-2.0

package principal

import (
	assertions "github.com/stretchr/testify/assert"
	"testing"
)

func TestNewManager(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	manager, err := NewManager()
	assert.NoError(err)
	assert.NotNil(manager)
	assert.IsType(&defaultManager{}, manager)
}

func TestDefaultManager_WithMock_UserExistsByName(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	security := &defaultManager{provider: &mockProvider{}}
	assert.True(security.UserExistsByName("validUser"))
	assert.False(security.UserExistsByName("xyz"))
}

func TestDefaultManager_WithMock_GroupExistsByName(t *testing.T) {
	// Simplify repetitive assertions by avoiding the need to repeat the testing.T argument.
	assert := assertions.New(t)

	security := &defaultManager{provider: &mockProvider{}}
	assert.True(security.GroupExistsByName("validGroup"))
	assert.False(security.GroupExistsByName("xyz"))
}
