package fsx

import (
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
	"reflect"
	"strconv"
)

//region OwnershipChangeError

const ownershipChangeErrorMsg string = "failed to change file or directory ownership [ path = '%s', user = '%s', group = '%s', recursive = '%t' ]"

// OwnershipChangeError is returned when a user cannot be found.
type OwnershipChangeError struct {
	path      string
	user      principal.User
	group     principal.Group
	recursive bool
	cause     error
}

func NewOwnershipChangeError(cause error, path string, user principal.User, group principal.Group, recursive bool) error {
	return &OwnershipChangeError{
		path:      path,
		user:      user,
		group:     group,
		recursive: recursive,
		cause:     cause,
	}
}

func (err *OwnershipChangeError) Path() string {
	return err.path
}

func (err *OwnershipChangeError) User() principal.User {
	return err.user
}

func (err *OwnershipChangeError) Group() principal.Group {
	return err.group
}

func (err *OwnershipChangeError) Recursive() bool {
	return err.recursive
}

func (err *OwnershipChangeError) Error() string {
	user := ""
	group := ""
	if err.user != nil {
		user = err.user.Name()
	}

	if err.group != nil {
		group = err.group.Name()
	}
	return fmt.Sprintf(ownershipChangeErrorMsg, err.path, user, group, err.recursive)
}

// SafeDetails emits a PII-safe slice.
func (err *OwnershipChangeError) SafeDetails() []string {
	user := ""
	group := ""
	if err.user != nil {
		user = err.user.Name()
	}

	if err.group != nil {
		group = err.group.Name()
	}
	return []string{err.path, user, group, strconv.FormatBool(err.recursive)}
}

// Unwrap returns the error cause from an
// instance of IllegalArgumentError
func (err *OwnershipChangeError) Unwrap() error {
	return err.cause
}

// Cause returns the root cause from an
// instance of error.
func (err *OwnershipChangeError) Cause() error {
	return err.cause
}

// Is returns true if the error is an IllegalArgError
func (err *OwnershipChangeError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(err)
}

// Format is called when printing errors via logging, etc
func (err *OwnershipChangeError) Format(f fmt.State, verb rune) {
	errors.FormatError(err, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (err *OwnershipChangeError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(err.Error())
	}

	return err.Cause()
}

//endregion
