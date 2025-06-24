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

const illegalArgErrorMsg = "The argument '%s' with a value of '%v' is invalid: '%s'"

// IllegalArgumentError maintains the fields necessary
// to track the details of this error.
type IllegalArgumentError struct {
	cause   error // Note: cause could be nil
	argName string
	reason  string
	value   interface{}
}

// NewIllegalArgumentError is a constructor for creating an
// IllegalArgumentError type error.
func NewIllegalArgumentError(cause error, argName string, reason string, value interface{}) error {

	return &IllegalArgumentError{
		cause:   cause,
		argName: argName,
		reason:  reason,
		value:   value,
	}
}

func (e *IllegalArgumentError) ArgName() string {
	return e.argName
}

func (e *IllegalArgumentError) Reason() string {
	return e.reason
}

func (e *IllegalArgumentError) Value() interface{} {
	return e.value
}

// Error returns a human-friendly error message.
func (e *IllegalArgumentError) Error() string {
	return fmt.Sprintf(illegalArgErrorMsg, e.ArgName(), e.Value(), e.Reason())
}

// SafeDetails emits a PII-safe slice.
func (e *IllegalArgumentError) SafeDetails() []string {
	return []string{e.ArgName(), e.Reason()}
}

// Unwrap returns the error cause from an
// instance of IllegalArgumentError
func (e *IllegalArgumentError) Unwrap() error {
	return e.cause
}

// Cause returns the root cause from an
// instance of error.
func (e *IllegalArgumentError) Cause() error {
	return e.cause
}

// Is returns true if the error is an IllegalArgError
func (e *IllegalArgumentError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(e)
}

// Format is called when printing errors via logging, etc
func (e *IllegalArgumentError) Format(f fmt.State, verb rune) {
	errors.FormatError(e, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (e *IllegalArgumentError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(e.Error())
	}

	return e.Cause()
}
