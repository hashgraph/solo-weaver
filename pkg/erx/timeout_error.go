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

const timeoutErrorMsg string = "The operation '%s' timed out."

// TimeoutError maintains the fields necessary
// to track the details of this error.
type TimeoutError struct {
	name string
}

// NewTimeoutError is a constructor for creating an
// TimeoutError type leaf error.
func NewTimeoutError(name string) error {
	return &TimeoutError{name: name}
}

func (e *TimeoutError) Name() string {
	return e.name
}

// Error returns a human-friendly error message.
func (e *TimeoutError) Error() string {
	return fmt.Sprintf(timeoutErrorMsg, e.Name())
}

// SafeDetails emits a PII-safe slice.
func (e *TimeoutError) SafeDetails() []string {
	return []string{e.Name()}
}

// Unwrap returns nil because this is a
// leaf error.
func (e *TimeoutError) Unwrap() error {
	return nil
}

// Cause returns nil because this is a
// leaf error.
func (e *TimeoutError) Cause() error {
	return nil
}

// Is returns true if the error is a TimeoutError
func (e *TimeoutError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(e)
}

// Format is called when printing errors via logging, etc
func (e *TimeoutError) Format(f fmt.State, verb rune) {
	errors.FormatError(e, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (e *TimeoutError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(e.Error())
	}

	return e.Cause()
}
