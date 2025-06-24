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
	"context"
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/errors/errbase"
	"github.com/cockroachdb/errors/errorspb"
	"github.com/gogo/protobuf/proto"
	"golang.hedera.com/solo-provisioner/pkg/exit"
	"reflect"
	"strconv"
)

const commandErrorMsg string = "%s - Exit Code: %d"

// CommandError binds an error with an exit.Code.
type CommandError struct {
	cause    error // Note: cause could be nil
	exitCode exit.Code
	msg      string
}

// NewCommandError is a constructor for creating a CommandError type
func NewCommandError(cause error, code exit.Code, msg string) error {

	if code < exit.MinValidExitCode || code > exit.MaxValidExitCode {
		code = exit.GeneralError
	}

	return &CommandError{cause: cause, exitCode: code, msg: msg}
}

func (e *CommandError) ExitCode() exit.Code {
	return e.exitCode
}

func (e *CommandError) Msg() string {
	return e.msg
}

// Error returns a human-friendly error message.
func (e *CommandError) Error() string {
	return fmt.Sprintf(commandErrorMsg, e.Msg(), e.ExitCode())
}

// Unwrap returns the error cause from an
// instance of CommandError.
func (e *CommandError) Unwrap() error {
	return e.cause
}

// Cause returns the root cause from an
// instance of error.
func (e *CommandError) Cause() error {
	return e.cause
}

// Is returns true if error is a CommandError.
func (e *CommandError) Is(target error) bool {
	return reflect.TypeOf(target) == reflect.TypeOf(e)
}

// Format is called when printing errors via logging, etc
func (e *CommandError) Format(f fmt.State, verb rune) {
	errors.FormatError(e, f, verb)
}

// FormatError is called when printing errors via logging, etc
func (e *CommandError) FormatError(p errbase.Printer) error {
	if p.Detail() {
		p.Print(e.Error())
	}

	return e.cause
}

func encodeError(_ context.Context, err error) (string, []string, proto.Message) {
	w := err.(*CommandError)
	return "", nil, &errorspb.StringPayload{Msg: w.ExitCode().String()}
}

func decodeError(_ context.Context, cause error, _ string, _ []string, payload proto.Message) error {
	m, ok := payload.(*errorspb.StringPayload)
	if !ok {
		return nil
	}

	ec, err := strconv.Atoi(m.Msg)
	if err != nil {
		ec = -1
	}

	return &CommandError{cause: cause, exitCode: exit.Code(ec)}
}

func init() {
	errbase.RegisterWrapperEncoder(errbase.GetTypeKey((*CommandError)(nil)), encodeError)
	errbase.RegisterWrapperDecoder(errbase.GetTypeKey((*CommandError)(nil)), decodeError)
}

// SafeDetails emits a PII-safe slice.
func (e *CommandError) SafeDetails() []string {
	return []string{string(rune(e.ExitCode())), e.Msg()}
}
