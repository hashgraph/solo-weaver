package principal

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"strconv"
	"testing"
)

const (
	testName        = "testuser"
	testDisplayName = "Test User"
	testUid         = 1000
	testGid         = 1000
	testHomeDir     = "/home/testuser"
	testShell       = "/bin/bash"
)

var (
	testMembers = []string{"testuser", "testuser2"}
	testUser    = &unixUser{
		manager:     nil,
		name:        testName,
		displayName: testDisplayName,
		uid:         testUid,
		gid:         testGid,
		homeDir:     testHomeDir,
		shell:       testShell,
	}
	testGroup = &unixGroup{
		manager: nil,
		name:    testName,
		gid:     testGid,
		members: testMembers,
	}
)

func TestUnixPrincipal_WithUnixUser_Uid(t *testing.T) {
	assert.Equal(t, strconv.Itoa(testUid), testUser.Uid())
}

func TestUnixPrincipal_WithUnixUser_Name(t *testing.T) {
	assert.Equal(t, testName, testUser.Name())
}

func TestUnixPrincipal_WithUnixUser_DisplayName(t *testing.T) {
	assert.Equal(t, testDisplayName, testUser.DisplayName())
}

func TestUnixPrincipal_WithUnixUser_HomeDir(t *testing.T) {
	assert.Equal(t, testHomeDir, testUser.HomeDir())
}

func TestUnixPrincipal_WithUnixUser_Shell(t *testing.T) {
	assert.Equal(t, testShell, testUser.Shell())
}

func TestUnixPrincipal_WithUnixUser_Validate(t *testing.T) {
	assert.NoError(t, testUser.Validate())
}

func TestUnixPrincipal_WithUnixUserAndNoName_Validate(t *testing.T) {
	noNameUser := &unixUser{
		name: "",
	}
	assert.Error(t, noNameUser.Validate())
}

func TestUnixPrincipal_WithUnixUserAndInvalidUid_Validate(t *testing.T) {
	invalidUidUser := &unixUser{
		name: testName,
		uid:  0,
	}
	assert.Error(t, invalidUidUser.Validate())
}

func TestUnixPrincipal_WithUnixUserAndInvalidGid_Validate(t *testing.T) {
	invalidUidUser := &unixUser{
		name: testName,
		uid:  testUid,
		gid:  0,
	}
	assert.Error(t, invalidUidUser.Validate())
}

func TestUnixPrincipal_WithUnixUserAndNoHomeDir_Validate(t *testing.T) {
	defaultHomeDirUser := &unixUser{
		name: testName,
		uid:  testUid,
		gid:  testGid,
	}
	assert.NoError(t, defaultHomeDirUser.Validate())
	assert.Equal(t, fmt.Sprintf("/home/%s", testName), defaultHomeDirUser.HomeDir())
}

func TestUnixPrincipal_WithUnixGroup_Gid(t *testing.T) {
	assert.Equal(t, strconv.Itoa(testGid), testGroup.Gid())
}

func TestUnixPrincipal_WithUnixGroup_Name(t *testing.T) {
	assert.Equal(t, testName, testGroup.Name())
}

func TestUnixPrincipal_WithUnixGroup_Validate(t *testing.T) {
	assert.NoError(t, testGroup.Validate())
}

func TestUnixPrincipal_WithUnixGroupAndNoName_Validate(t *testing.T) {
	noNameGroup := &unixGroup{
		name: "",
	}
	assert.Error(t, noNameGroup.Validate())
}

func TestUnixPrincipal_WithUnixGroupAndInvalidGid_Validate(t *testing.T) {
	invalidGidGroup := &unixGroup{
		name: testName,
		gid:  0,
	}
	assert.Error(t, invalidGidGroup.Validate())
}
