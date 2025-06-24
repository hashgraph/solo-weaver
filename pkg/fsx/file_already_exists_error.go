package fsx

import (
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
	"reflect"
)

//region FileAlreadyExistsError

const fileAlreadyExistsErrorMsg string = "file already exists: %s"

// FileAlreadyExistsError is returned when the supplied path already exists. This error can occur when creating, copying,
// moving, or renaming a file or directory. In some cases this error may be returned when the overwrite flag is set to
// false.
type FileAlreadyExistsError struct {
	path  string
	cause error
}

func NewFileAlreadyExistsError(cause error, path string) error {
	return &FileAlreadyExistsError{
		path:  path,
		cause: cause,
	}
}

func (err *FileAlreadyExistsError) Path() string {
	return err.path
}

func (err *FileAlreadyExistsError) Error() string {
	return fmt.Sprintf(fileAlreadyExistsErrorMsg, err.path)
}

// SafeDetails emits a PII-safe slice.
func (err *FileAlreadyExistsError) SafeDetails() []string {
	return []string{err.path}
}

// Unwrap returns the error cause from an
// instance of IllegalArgumentError
func (err *FileAlreadyExistsError) Unwrap() error {
	return err.cause
}

// Cause returns the root cause from an
// instance of error.
func (err *FileAlreadyExistsError) Cause() error {
	return err.cause
}

// Is returns true if the error is an IllegalArgError
func (err *FileAlreadyExistsError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(err)
}

// Format is called when printing errors via logging, etc
func (err *FileAlreadyExistsError) Format(f fmt.State, verb rune) {
	errors.FormatError(err, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (err *FileAlreadyExistsError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(err.Error())
	}

	return err.Cause()
}

//endregion
