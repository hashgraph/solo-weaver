package principal

import (
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
	"reflect"
)

//region UserNotFoundError

// UserNotFoundError is returned when a user cannot be found.
type UserNotFoundError struct {
	name  string
	uid   string
	cause error
}

func NewUserNotFoundError(cause error, name string, uid string) error {
	return &UserNotFoundError{
		name:  name,
		uid:   uid,
		cause: cause,
	}
}

func (err *UserNotFoundError) Name() string {
	return err.name
}

func (err *UserNotFoundError) Uid() string {
	return err.uid
}

func (err *UserNotFoundError) Error() string {
	if len(err.name) > 0 {
		return fmt.Sprintf("User with name `%s` not found!", err.name)
	} else {
		return fmt.Sprintf("User with uid `%s` not found!", err.uid)
	}
}

// SafeDetails emits a PII-safe slice.
func (err *UserNotFoundError) SafeDetails() []string {
	return []string{err.uid, err.name}
}

// Unwrap returns the error cause from an
// instance of IllegalArgumentError
func (err *UserNotFoundError) Unwrap() error {
	return err.cause
}

// Cause returns the root cause from an
// instance of error.
func (err *UserNotFoundError) Cause() error {
	return err.cause
}

// Is returns true if the error is an IllegalArgError
func (err *UserNotFoundError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(err)
}

// Format is called when printing errors via logging, etc
func (err *UserNotFoundError) Format(f fmt.State, verb rune) {
	errors.FormatError(err, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (err *UserNotFoundError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(err.Error())
	}

	return err.Cause()
}

//endregion

//region GroupNotFoundError

// GroupNotFoundError is returned when a group cannot be found.
type GroupNotFoundError struct {
	name  string
	gid   string
	cause error
}

func NewGroupNotFoundError(cause error, name string, gid string) error {
	return &GroupNotFoundError{
		name:  name,
		gid:   gid,
		cause: cause,
	}
}

func (err *GroupNotFoundError) Name() string {
	return err.name
}

func (err *GroupNotFoundError) Gid() string {
	return err.gid
}

func (err *GroupNotFoundError) Error() string {
	if len(err.name) > 0 {
		return fmt.Sprintf("Group with name `%s` not found!", err.name)
	} else {
		return fmt.Sprintf("Group with gid `%s` not found!", err.gid)
	}
}

// SafeDetails emits a PII-safe slice.
func (err *GroupNotFoundError) SafeDetails() []string {
	return []string{err.gid, err.name}
}

// Unwrap returns the error cause from an
// instance of IllegalArgumentError
func (err *GroupNotFoundError) Unwrap() error {
	return err.cause
}

// Cause returns the root cause from an
// instance of error.
func (err *GroupNotFoundError) Cause() error {
	return err.cause
}

// Is returns true if the error is an IllegalArgError
func (err *GroupNotFoundError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(err)
}

// Format is called when printing errors via logging, etc
func (err *GroupNotFoundError) Format(f fmt.State, verb rune) {
	errors.FormatError(err, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (err *GroupNotFoundError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(err.Error())
	}

	return err.Cause()
}

//endregion
