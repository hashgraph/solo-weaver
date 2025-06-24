package fsx

import (
	"fmt"
	"reflect"

	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
)

// region FileSystemError

const fileSystemErrorMsg string = "file system error: %s (%s)"

// FileSystemError is returned when an OS level file system error occurs and could not be handled by the specific method.
// The details field provides additional information or context about the error.
type FileSystemError struct {
	path    string
	details string
	cause   error
}

func NewFileSystemError(cause error, details string, path string) error {
	return errors.WithStack(&FileSystemError{
		path:    path,
		details: details,
		cause:   cause,
	})
}

func (err *FileSystemError) Path() string {
	return err.path
}

func (err *FileSystemError) Details() string {
	return err.details
}

func (err *FileSystemError) Error() string {
	return fmt.Sprintf(fileSystemErrorMsg, err.details, err.path)
}

// SafeDetails emits a PII-safe slice.
func (err *FileSystemError) SafeDetails() []string {
	return []string{err.details, err.path}
}

// Unwrap returns the error cause from an
// instance of IllegalArgumentError
func (err *FileSystemError) Unwrap() error {
	return err.cause
}

// Cause returns the root cause from an
// instance of error.
func (err *FileSystemError) Cause() error {
	return err.cause
}

// Is returns true if the error is an IllegalArgError
func (err *FileSystemError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(err)
}

// Format is called when printing errors via logging, etc
func (err *FileSystemError) Format(f fmt.State, verb rune) {
	errors.FormatError(err, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (err *FileSystemError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(err.Error())
	}

	return err.Cause()
}

// endregion
