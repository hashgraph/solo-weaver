package fsx

import (
	"fmt"
	"github.com/golang/mock/gomock"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
	"strconv"
	"testing"
)

func TestOwnershipChangeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	path := "/some/path/to/file"
	user := principal.NewMockUser(ctrl)
	group := principal.NewMockGroup(ctrl)
	user.EXPECT().Name().Return("user").AnyTimes()
	group.EXPECT().Name().Return("group").AnyTimes()
	recursive := true
	expected := fmt.Sprintf(ownershipChangeErrorMsg, path, user.Name(), group.Name(), recursive)
	cause := fmt.Errorf("test error cause")

	t.Run("without cause", func(t *testing.T) {
		err := NewOwnershipChangeError(nil, path, user, group, recursive)
		require.NotNil(t, err)
		require.NotEmpty(t, err)
		require.Equal(t, expected, err.Message())
		require.NotContains(t, err.Error(), cause.Error())
		assertErrorProperties(t, err, map[errorx.Property]interface{}{
			pathProperty:      path,
			userProperty:      user.Name(),
			groupProperty:     group.Name(),
			recursiveProperty: recursive,
		})
		details := SafeErrorDetails(err)
		require.Equal(t, []string{path, "user", "group", strconv.FormatBool(recursive)}, details)
		require.True(t, errorx.IsOfType(err, OwnershipChangeError))
		require.True(t, OwnershipChangeError.IsOfType(err.Type()))
		require.True(t, err.IsOfType(OwnershipChangeError))
	})

	t.Run("with cause", func(t *testing.T) {
		err := NewOwnershipChangeError(cause, path, user, group, recursive)
		require.NotNil(t, err)
		require.NotEmpty(t, err)
		require.Equal(t, expected, err.Message())
		require.Contains(t, err.Error(), cause.Error())
		assertErrorProperties(t, err, map[errorx.Property]interface{}{
			pathProperty:      path,
			userProperty:      user.Name(),
			groupProperty:     group.Name(),
			recursiveProperty: recursive,
		})
	})
}

func TestPermissionChangeError(t *testing.T) {
	path := "/some/path/to/file"
	perms := uint(0644)
	recursive := true
	expected := fmt.Sprintf(permissionChangeErrorMsg, path, strconv.FormatUint(uint64(perms), 8), recursive)
	cause := fmt.Errorf("test error cause")

	t.Run("without cause", func(t *testing.T) {
		err := NewPermissionChangeError(nil, path, perms, recursive)
		require.NotNil(t, err)
		require.NotEmpty(t, err)
		require.Equal(t, expected, err.Message())
		require.NotContains(t, err.Error(), cause.Error())
		assertErrorProperties(t, err, map[errorx.Property]interface{}{
			pathProperty:      path,
			permsProperty:     perms,
			recursiveProperty: recursive,
		})
		details := SafeErrorDetails(err)
		require.Equal(t, []string{path, strconv.FormatUint(uint64(perms), 8), strconv.FormatBool(recursive)}, details)
		require.True(t, errorx.IsOfType(err, PermissionChangeError))
		require.True(t, PermissionChangeError.IsOfType(err.Type()))
		require.True(t, err.IsOfType(PermissionChangeError))
	})

	t.Run("with cause", func(t *testing.T) {
		err := NewPermissionChangeError(cause, path, perms, recursive)
		require.NotNil(t, err)
		require.NotEmpty(t, err)
		require.Equal(t, expected, err.Message())
		require.Contains(t, err.Error(), cause.Error())
		assertErrorProperties(t, err, map[errorx.Property]interface{}{
			pathProperty:      path,
			permsProperty:     perms,
			recursiveProperty: recursive,
		})
	})
}

func TestSafeErrorDetails(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		require.Empty(t, SafeErrorDetails(nil))
	})

	t.Run("unrelated error", func(t *testing.T) {
		typ := errorx.RegisterPrintableProperty("other")
		err := errorx.NewType(errorx.NewNamespace("ns"), "t").New("msg").WithProperty(typ, "value")
		require.Empty(t, SafeErrorDetails(err))
	})
}

// assertErrorProperties checks that all expected properties are present and correct.
func assertErrorProperties(t *testing.T, err *errorx.Error, expected map[errorx.Property]interface{}) {
	for prop, want := range expected {
		got, ok := err.Property(prop)
		require.True(t, ok, "Property %v not found", prop)
		require.Equal(t, want, got, "Property %v mismatch", prop)
	}
}
