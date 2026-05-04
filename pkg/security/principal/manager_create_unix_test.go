// SPDX-License-Identifier: Apache-2.0

//go:build !darwin && !windows

package principal

import (
	"fmt"
	"io"
	"os"
	"os/user"
	"strings"
	"testing"

	assertions "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestProvider is a minimal Provider for testing Create* methods.
// writeGroupEntryErr / writeUserEntryErr control what the Write calls return.
// groups / users are the slices returned by EnumerateGroups/EnumerateUsers before a
// Write call. groupsAfterWrite / usersAfterWrite are returned after the respective
// Write succeeds, simulating what the OS sees once the entry has been committed.
type createTestProvider struct {
	writeGroupEntryErr  error
	writeUserEntryErr   error
	addMemberToGroupErr error
	groups              []Group
	groupsAfterWrite    []Group
	groupWritten        bool
	users               []User
	usersAfterWrite     []User
	userWritten         bool
	memberAdded         bool
}

func (p *createTestProvider) EnumerateGroups(_ Manager) ([]Group, error) {
	if p.groupWritten && p.groupsAfterWrite != nil {
		return p.groupsAfterWrite, nil
	}
	return p.groups, nil
}

func (p *createTestProvider) EnumerateUsers(_ Manager) ([]User, error) {
	if p.userWritten && p.usersAfterWrite != nil {
		return p.usersAfterWrite, nil
	}
	return p.users, nil
}

func (p *createTestProvider) WriteGroupEntry(_ string, _ int) error {
	if p.writeGroupEntryErr != nil {
		return p.writeGroupEntryErr
	}
	p.groupWritten = true
	return nil
}

func (p *createTestProvider) WriteGroupShadowEntry(_ string) error { return nil }

func (p *createTestProvider) WriteUserEntry(_ string, _ int, _ string) error {
	if p.writeUserEntryErr != nil {
		return p.writeUserEntryErr
	}
	p.userWritten = true
	return nil
}

func (p *createTestProvider) WriteUserShadowEntry(_ string) error { return nil }

func (p *createTestProvider) AddMemberToGroup(_ string, _ string) error {
	if p.addMemberToGroupErr != nil {
		return p.addMemberToGroupErr
	}
	p.memberAdded = true
	return nil
}

// makeTempIDFile creates a temporary colon-delimited identity file with the given content
// and returns it seeked to the beginning, ready for nextFreeID.
func makeTempIDFile(t *testing.T, content string) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "ids")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// ---- nextFreeID ----

func TestNextFreeID_EmptyFile_ReturnsMin(t *testing.T) {
	f := makeTempIDFile(t, "")
	id, err := nextFreeID(f)
	require.NoError(t, err)
	assertions.New(t).Equal(sysIDMin, id)
}

func TestNextFreeID_SkipsUsedIDs(t *testing.T) {
	// IDs 100, 101, 102 occupied; 103 is the first free one.
	content := "a:x:100:\nb:x:101:\nc:x:102:\n"
	f := makeTempIDFile(t, content)
	id, err := nextFreeID(f)
	require.NoError(t, err)
	assertions.New(t).Equal(103, id)
}

func TestNextFreeID_IgnoresOutOfRangeIDs(t *testing.T) {
	// UIDs 0 and 65534 are outside [sysIDMin, sysIDMax] and must not block allocation.
	content := "root:x:0:\nnobody:x:65534:\n"
	f := makeTempIDFile(t, content)
	id, err := nextFreeID(f)
	require.NoError(t, err)
	assertions.New(t).Equal(sysIDMin, id)
}

func TestNextFreeID_IgnoresMalformedLines(t *testing.T) {
	// Lines with fewer than 3 fields or a non-numeric ID field are silently skipped.
	content := "malformed\na:x:notanumber:\nb:x:100:\n"
	f := makeTempIDFile(t, content)
	id, err := nextFreeID(f)
	require.NoError(t, err)
	assertions.New(t).Equal(101, id) // 100 used, 101 free
}

func TestNextFreeID_RangeExhausted_ReturnsError(t *testing.T) {
	var sb strings.Builder
	for i := sysIDMin; i <= sysIDMax; i++ {
		_, _ = fmt.Fprintf(&sb, "u%d:x:%d:\n", i, i)
	}
	f := makeTempIDFile(t, sb.String())
	_, err := nextFreeID(f)
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "no free system ID")
}

// ---- CreateGroupWithId ----

func TestCreateGroupWithId_Success(t *testing.T) {
	g := &mockGroup{group: &user.Group{Gid: "200", Name: "testgroup"}}
	p := &createTestProvider{
		groups:           []Group{},  // empty before write: group does not exist yet
		groupsAfterWrite: []Group{g}, // returned after WriteGroupEntry succeeds
	}
	m := &defaultManager{provider: p}

	result, err := m.CreateGroupWithId("testgroup", 200)
	require.NoError(t, err)
	assertions.New(t).Equal("testgroup", result.Name())
}

func TestCreateGroupWithId_WriteEntryFails_ReturnsError(t *testing.T) {
	p := &createTestProvider{writeGroupEntryErr: fmt.Errorf("disk full")}
	m := &defaultManager{provider: p}

	_, err := m.CreateGroupWithId("testgroup", 200)
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "failed to create group")
}

func TestCreateGroupWithId_NameAlreadyExists_ReturnsError(t *testing.T) {
	existing := &mockGroup{group: &user.Group{Gid: "200", Name: "testgroup"}}
	p := &createTestProvider{groups: []Group{existing}}
	m := &defaultManager{provider: p}

	_, err := m.CreateGroupWithId("testgroup", 200)
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "already exists")
}

func TestCreateGroupWithId_GIDConflict_ReturnsError(t *testing.T) {
	// GID 200 is already owned by "othergroup"; creating a new group with the same GID must fail.
	existing := &mockGroup{group: &user.Group{Gid: "200", Name: "othergroup"}}
	p := &createTestProvider{groups: []Group{existing}}
	m := &defaultManager{provider: p}

	_, err := m.CreateGroupWithId("testgroup", 200)
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "already used by group")
}

func TestCreateGroupWithId_GroupAbsentAfterRefresh_ReturnsError(t *testing.T) {
	// WriteGroupEntry succeeds but the group does not appear in EnumerateGroups
	// (e.g. the entry was written to the wrong file).  LookupGroupByName must fail.
	p := &createTestProvider{groups: []Group{}} // empty — group not found after refresh
	m := &defaultManager{provider: p}

	_, err := m.CreateGroupWithId("testgroup", 200)
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "testgroup")
}

// ---- CreateUserWithId ----

func TestCreateUserWithId_Success(t *testing.T) {
	u := &mockUser{user: &user.User{Uid: "300", Username: "testuser"}}
	p := &createTestProvider{
		users:           []User{},  // empty before write: user does not exist yet
		usersAfterWrite: []User{u}, // returned after WriteUserEntry succeeds
	}
	m := &defaultManager{provider: p}

	result, err := m.CreateUserWithId("testuser", 300, "/")
	require.NoError(t, err)
	assertions.New(t).Equal("testuser", result.Name())
}

func TestCreateUserWithId_WriteEntryFails_ReturnsError(t *testing.T) {
	p := &createTestProvider{writeUserEntryErr: fmt.Errorf("permission denied")}
	m := &defaultManager{provider: p}

	_, err := m.CreateUserWithId("testuser", 300, "/")
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "failed to create user")
}

func TestCreateUserWithId_NameAlreadyExists_ReturnsError(t *testing.T) {
	existing := &mockUser{user: &user.User{Uid: "300", Username: "testuser"}}
	p := &createTestProvider{users: []User{existing}}
	m := &defaultManager{provider: p}

	_, err := m.CreateUserWithId("testuser", 300, "/")
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "already exists")
}

func TestCreateUserWithId_UIDConflict_ReturnsError(t *testing.T) {
	// UID 300 is already owned by "otheruser"; creating a new user with the same UID must fail.
	existing := &mockUser{user: &user.User{Uid: "300", Username: "otheruser"}}
	p := &createTestProvider{users: []User{existing}}
	m := &defaultManager{provider: p}

	_, err := m.CreateUserWithId("testuser", 300, "/")
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "already used by user")
}

func TestCreateUserWithId_UserAbsentAfterRefresh_ReturnsError(t *testing.T) {
	// WriteUserEntry succeeds but the user does not appear in EnumerateUsers.
	p := &createTestProvider{users: []User{}}
	m := &defaultManager{provider: p}

	_, err := m.CreateUserWithId("testuser", 300, "/")
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "testuser")
}

// ---- AddUserToGroup ----

// mockGroupWithMembers is a mockGroup whose Users() returns a configurable slice.
type mockGroupWithMembers struct {
	mockGroup
	members []User
}

func (g *mockGroupWithMembers) Users() []User { return g.members }

func TestAddUserToGroup_Success(t *testing.T) {
	u := &mockUser{user: &user.User{Uid: "300", Username: "testuser"}}
	g := &mockGroupWithMembers{
		mockGroup: mockGroup{group: &user.Group{Gid: "200", Name: "testgroup"}},
		members:   []User{}, // testuser not yet a member
	}
	p := &createTestProvider{
		users:  []User{u},
		groups: []Group{g},
	}
	m := &defaultManager{provider: p}

	err := m.AddUserToGroup("testuser", "testgroup")
	require.NoError(t, err)
	assertions.New(t).True(p.memberAdded)
}

func TestAddUserToGroup_AlreadyMember_SkipsProvider(t *testing.T) {
	u := &mockUser{user: &user.User{Uid: "300", Username: "testuser"}}
	g := &mockGroupWithMembers{
		mockGroup: mockGroup{group: &user.Group{Gid: "200", Name: "testgroup"}},
		members:   []User{u}, // testuser already a member
	}
	p := &createTestProvider{
		users:  []User{u},
		groups: []Group{g},
	}
	m := &defaultManager{provider: p}

	err := m.AddUserToGroup("testuser", "testgroup")
	require.NoError(t, err)
	assertions.New(t).False(p.memberAdded, "provider must not be called when user is already a member")
}

func TestAddUserToGroup_UserNotFound_ReturnsError(t *testing.T) {
	p := &createTestProvider{users: []User{}}
	m := &defaultManager{provider: p}

	err := m.AddUserToGroup("nobody", "testgroup")
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "nobody")
}

func TestAddUserToGroup_GroupNotFound_ReturnsError(t *testing.T) {
	u := &mockUser{user: &user.User{Uid: "300", Username: "testuser"}}
	p := &createTestProvider{
		users:  []User{u},
		groups: []Group{},
	}
	m := &defaultManager{provider: p}

	err := m.AddUserToGroup("testuser", "nogroup")
	require.Error(t, err)
	assertions.New(t).Contains(err.Error(), "nogroup")
}
