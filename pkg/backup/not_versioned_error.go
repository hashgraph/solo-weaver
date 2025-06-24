package backup

import (
	"fmt"
	"reflect"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
)

// region NotVersionedError

const notVersionedErrorMsg string = "the specified backup path is not versioned: %s"

// NotVersionedError is returned when the supplied path already exists. This error can occur when creating, copying,
// moving, or renaming a file or directory. In some cases this error may be returned when the overwrite flag is set to
// false.
type NotVersionedError struct {
	path  string
	cause error
}

func NewNotVersionedError(cause error, path string) error {
	return errors.WithStack(&NotVersionedError{
		path:  path,
		cause: cause,
	})
}

func (err *NotVersionedError) Path() string {
	return err.path
}

func (err *NotVersionedError) Error() string {
	return fmt.Sprintf(notVersionedErrorMsg, err.path)
}

// SafeDetails emits a PII-safe slice.
func (err *NotVersionedError) SafeDetails() []string {
	return []string{err.path}
}

// Unwrap returns the error cause from an
// instance of IllegalArgumentError
func (err *NotVersionedError) Unwrap() error {
	return err.cause
}

// Cause returns the root cause from an
// instance of error.
func (err *NotVersionedError) Cause() error {
	return err.cause
}

// Is returns true if the error is an IllegalArgError
func (err *NotVersionedError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(err)
}

// Format is called when printing errors via logging, etc
func (err *NotVersionedError) Format(f fmt.State, verb rune) {
	errors.FormatError(err, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (err *NotVersionedError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(err.Error())
	}

	return err.Cause()
}

// endregion
