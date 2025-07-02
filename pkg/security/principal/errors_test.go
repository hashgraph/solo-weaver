package principal

import (
	"fmt"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

func TestNewUserNotFoundError(t *testing.T) {
	name := "alice"
	uid := "1001"
	cause := fmt.Errorf("lookup failed")

	t.Run("without cause", func(t *testing.T) {
		err := NewUserNotFoundError(nil, name, uid)
		require.NotNil(t, err)
		require.Contains(t, err.Error(), name)
		require.Contains(t, err.Error(), uid)
		assertErrorProperties(t, err, map[errorx.Property]interface{}{
			nameProperty: name,
			uidProperty:  uid,
		})
		require.True(t, errorx.IsOfType(err, UserNotFoundError))
	})

	t.Run("with cause", func(t *testing.T) {
		err := NewUserNotFoundError(cause, name, uid)
		require.NotNil(t, err)
		require.Contains(t, err.Error(), name)
		require.Contains(t, err.Error(), uid)
		require.Contains(t, err.Error(), cause.Error())
		assertErrorProperties(t, err, map[errorx.Property]interface{}{
			nameProperty: name,
			uidProperty:  uid,
		})
		require.True(t, errorx.IsOfType(err, UserNotFoundError))
	})
}

func TestNewGroupNotFoundError(t *testing.T) {
	name := "devs"
	gid := "2001"
	cause := fmt.Errorf("lookup failed")

	t.Run("without cause", func(t *testing.T) {
		err := NewGroupNotFoundError(nil, name, gid)
		require.NotNil(t, err)
		require.Contains(t, err.Error(), name)
		require.Contains(t, err.Error(), gid)
		assertErrorProperties(t, err, map[errorx.Property]interface{}{
			nameProperty: name,
			gidProperty:  gid,
		})
		require.True(t, errorx.IsOfType(err, GroupNotFoundError))
	})

	t.Run("with cause", func(t *testing.T) {
		err := NewGroupNotFoundError(cause, name, gid)
		require.NotNil(t, err)
		require.Contains(t, err.Error(), name)
		require.Contains(t, err.Error(), gid)
		require.Contains(t, err.Error(), cause.Error())
		assertErrorProperties(t, err, map[errorx.Property]interface{}{
			nameProperty: name,
			gidProperty:  gid,
		})
		require.True(t, errorx.IsOfType(err, GroupNotFoundError))
	})
}

func TestSafeErrorDetails(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		require.Empty(t, SafeErrorDetails(nil))
	})

	t.Run("user error", func(t *testing.T) {
		err := NewUserNotFoundError(nil, "bob", "1010")
		details := SafeErrorDetails(err)
		require.Equal(t, []string{"bob", "1010"}, details)
	})

	t.Run("group error", func(t *testing.T) {
		err := NewGroupNotFoundError(nil, "ops", "2020")
		details := SafeErrorDetails(err)
		require.Equal(t, []string{"ops", "2020"}, details)
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
