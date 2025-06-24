package fsx

import (
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
	"reflect"
)

//region FileTypeError

type ExpectedFileType string

const (
	Unknown         ExpectedFileType = ""
	Device          ExpectedFileType = "device"
	Directory       ExpectedFileType = "directory"
	File            ExpectedFileType = "file"
	FileOrDirectory ExpectedFileType = "file or directory"
	Hardlink        ExpectedFileType = "hardlink"
	Symlink         ExpectedFileType = "symlink"
	Socket          ExpectedFileType = "socket"
)

const fileTypeErrorMsg string = "file type error, expected the path to be a %s (%s)"

// FileTypeError is returned when the specified path is not the expected file type.
type FileTypeError struct {
	path         string
	expectedType ExpectedFileType
	cause        error
}

func NewFileTypeError(cause error, expectedType ExpectedFileType, path string) error {
	return &FileTypeError{
		path:         path,
		expectedType: expectedType,
		cause:        cause,
	}
}

func (err *FileTypeError) Path() string {
	return err.path
}

func (err *FileTypeError) ExpectedType() ExpectedFileType {
	return err.expectedType
}

func (err *FileTypeError) Error() string {
	return fmt.Sprintf(fileTypeErrorMsg, err.expectedType, err.path)
}

// SafeDetails emits a PII-safe slice.
func (err *FileTypeError) SafeDetails() []string {
	return []string{string(err.expectedType), err.path}
}

// Unwrap returns the error cause from an
// instance of IllegalArgumentError
func (err *FileTypeError) Unwrap() error {
	return err.cause
}

// Cause returns the root cause from an
// instance of error.
func (err *FileTypeError) Cause() error {
	return err.cause
}

// Is returns true if the error is an IllegalArgError
func (err *FileTypeError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(err)
}

// Format is called when printing errors via logging, etc
func (err *FileTypeError) Format(f fmt.State, verb rune) {
	errors.FormatError(err, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (err *FileTypeError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(err.Error())
	}

	return err.Cause()
}

//endregion
