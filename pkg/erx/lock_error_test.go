package erx

import (
	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

const msg = "There was an error"

func TestLockError_LeafError(t *testing.T) {
	req := require.New(t)
	err := NewLockError(nil, msg)
	req.NotEmpty(err)
	req.EqualError(err, msg)
	req.Equal(msg, err.(*LockError).Msg())
	req.Equal(msg, err.(*LockError).Error())
}

func TestLockError_NonLeafError(t *testing.T) {
	req := require.New(t)
	err := NewLockError(errors.New("root error"), msg)
	req.NotEmpty(err)
	req.Error(err, msg)
}

func TestLockError_SafeDetails(t *testing.T) {
	req := require.New(t)
	err := NewLockError(nil, msg)
	details := err.(*LockError).SafeDetails()
	req.Equal(msg, details[0])
}

func TestLockError_Unwrap(t *testing.T) {
	req := require.New(t)
	err := NewLockError(nil, msg)
	req.NotEmpty(err)
	req.Empty(err.(*LockError).Unwrap())

	rootErr := errors.New("root error")
	err = NewLockError(rootErr, msg)
	req.NotEmpty(err)
	req.EqualError(err.(*LockError).Unwrap(), "root error")
}

func TestLockError_Cause(t *testing.T) {
	req := require.New(t)
	err := NewLockError(nil, msg)
	req.NotEmpty(err)
	req.Empty(err.(*LockError).Cause())

	rootErr := errors.New("root error")
	err = NewLockError(rootErr, msg)
	req.NotEmpty(err)
	req.EqualError(err.(*LockError).Cause(), "root error")
}

func TestLockError_Is(t *testing.T) {
	req := require.New(t)
	err := NewLockError(nil, msg)
	req.True(errors.Is(err, &LockError{}))

	rootErr := errors.New("root error")
	err = NewLockError(rootErr, msg)
	req.True(errors.Is(err, &LockError{}))
}
