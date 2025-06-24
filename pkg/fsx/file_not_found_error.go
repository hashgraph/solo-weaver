package fsx

import (
	"fmt"
	"reflect"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
)

// region FileNotFoundError

const fileNotFoundErrorMsg string = "file not found: %s"

// FileNotFoundError is returned when the requested file or directory cannot be found.
type FileNotFoundError struct {
	path  string
	cause error
}

func NewFileNotFoundError(cause error, path string) error {
	return errors.WithStack(&FileNotFoundError{
		path:  path,
		cause: cause,
	})
}

func (err *FileNotFoundError) Path() string {
	return err.path
}

func (err *FileNotFoundError) Error() string {
	return fmt.Sprintf(fileNotFoundErrorMsg, err.path)
}

// SafeDetails emits a PII-safe slice.
func (err *FileNotFoundError) SafeDetails() []string {
	return []string{err.path}
}

// Unwrap returns the error cause from an
// instance of IllegalArgumentError
func (err *FileNotFoundError) Unwrap() error {
	return err.cause
}

// Cause returns the root cause from an
// instance of error.
func (err *FileNotFoundError) Cause() error {
	return err.cause
}

// Is returns true if the error is an IllegalArgError
func (err *FileNotFoundError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(err)
}

// Format is called when printing errors via logging, etc
func (err *FileNotFoundError) Format(f fmt.State, verb rune) {
	errors.FormatError(err, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (err *FileNotFoundError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(err.Error())
	}

	return err.Cause()
}

// endregion
