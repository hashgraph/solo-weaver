/*
 * Copyright 2016-2022 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package erx

import (
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
	"reflect"
)

// LockError maintains the fields necessary
// to track the details of this error.
type LockError struct {
	cause error
	msg   string
}

// NewLockError is a constructor for creating a
// Lock type error.
func NewLockError(cause error, msg string) error {

	return &LockError{cause: cause, msg: msg}
}

func (e *LockError) Msg() string {
	return e.msg
}

// Error returns a human-friendly error message.
func (e *LockError) Error() string {
	return e.msg
}

// Unwrap returns the error cause from an
// instance of LockError.
func (e *LockError) Unwrap() error {
	return e.cause
}

// Cause returns the root cause from an
// instance of error.
func (e *LockError) Cause() error {
	return e.cause
}

// Is returns true if error is a LockError.
func (e *LockError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(e)
}

// Format is called when printing errors via logging, etc
func (e *LockError) Format(f fmt.State, verb rune) {
	errors.FormatError(e, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (e *LockError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(e.Error())
	}

	return e.cause
}

// SafeDetails emits a PII-safe slice.
func (e *LockError) SafeDetails() []string {
	return []string{e.Msg()}
}
