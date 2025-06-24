package fsx

import (
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
	"reflect"
	"strconv"
)

//region PermissionChangeError

const permissionChangeErrorMsg = "failed to change file or directory permissions [ path = '%s', perms = '%s', recursive = '%t' ]"

// PermissionChangeError is returned when a user cannot be found.
type PermissionChangeError struct {
	path      string
	perms     uint
	recursive bool
	cause     error
}

func NewPermissionChangeError(cause error, path string, perms uint, recursive bool) error {
	return &PermissionChangeError{
		path:      path,
		perms:     perms,
		recursive: recursive,
		cause:     cause,
	}
}

func (err *PermissionChangeError) Path() string {
	return err.path
}

func (err *PermissionChangeError) Perms() uint {
	return err.perms
}

func (err *PermissionChangeError) Recursive() bool {
	return err.recursive
}

func (err *PermissionChangeError) Error() string {
	return fmt.Sprintf(permissionChangeErrorMsg, err.path, strconv.FormatUint(uint64(err.perms), 8), err.recursive)
}

// SafeDetails emits a PII-safe slice.
func (err *PermissionChangeError) SafeDetails() []string {
	return []string{err.path, strconv.FormatUint(uint64(err.perms), 8), strconv.FormatBool(err.recursive)}
}

// Unwrap returns the error cause from an
// instance of IllegalArgumentError
func (err *PermissionChangeError) Unwrap() error {
	return err.cause
}

// Cause returns the root cause from an
// instance of error.
func (err *PermissionChangeError) Cause() error {
	return err.cause
}

// Is returns true if the error is an IllegalArgError
func (err *PermissionChangeError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(err)
}

// Format is called when printing errors via logging, etc
func (err *PermissionChangeError) Format(f fmt.State, verb rune) {
	errors.FormatError(err, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (err *PermissionChangeError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(err.Error())
	}

	return err.Cause()
}

//endregion
