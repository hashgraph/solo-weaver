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
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/exit"
	"testing"
)

var errMsg1 = "Error calling to check the OS - Exit Code: 71"
var errMsg2 = "The operating system 'Windows NT' is not supported."

var testErrorMsg = "test error message"

func TestCommandError_HappyPath(t *testing.T) {
	req := require.New(t)

	var v struct{}
	set := make(map[string]struct{})
	set[errMsg1] = v
	set[errMsg2] = v

	msg := "Error calling to check the OS"
	unsupportedOsErr := NewUnsupportedOSError("Windows NT")
	commandErr := NewCommandError(unsupportedOsErr, exit.SystemError, msg)

	req.NotEmpty(commandErr)
}

func TestCommandError_ExitCode(t *testing.T) {
	req := require.New(t)

	testErrorMsg := "Test errors message"
	nestedErr := NewUnsupportedOSError("Windows NT")
	e := NewCommandError(nestedErr, 1, testErrorMsg)

	req.Equal(e.(*CommandError).ExitCode(), exit.GeneralError)
}

func TestCommandError_Cause(t *testing.T) {
	req := require.New(t)

	originalErrMsg := "Original error message"

	err := NewCommandError(errors.New(originalErrMsg), 1, testErrorMsg)
	req.NotEmpty(err.(*CommandError).Cause())
	req.Equal(originalErrMsg, err.(*CommandError).Cause().Error())

	err = NewCommandError(nil, 1, testErrorMsg)
	req.Nil(err.(*CommandError).Cause())
	req.Equal(fmt.Sprintf(commandErrorMsg, testErrorMsg, 1), err.(*CommandError).Error())
}

func TestCommandError_Unwrap(t *testing.T) {
	req := require.New(t)
	originalErrMsg := "Original error message"

	err := NewCommandError(errors.New(originalErrMsg), 1, testErrorMsg)
	req.NotEmpty(err.(*CommandError).Unwrap())
	req.Equal(originalErrMsg, err.(*CommandError).Unwrap().Error())
}

func TestCommandError_decodeError(t *testing.T) {
	req := require.New(t)
	testErrorMsg := "Test errors message"
	nestedErr := NewUnsupportedOSError("Windows NT")
	err := NewCommandError(nestedErr, 1, testErrorMsg)
	decodeError := decodeError(context.Background(), err, "", []string{}, nil)

	req.Empty(decodeError)
}

func TestCommandError_encodeError(t *testing.T) {
	req := require.New(t)
	err := NewCommandError(errors.New(testErrorMsg), 2, testErrorMsg)
	msg, strs, message := encodeError(context.Background(), err)

	req.Empty(msg)
	req.Empty(strs)
	req.Equal(message.String(), "msg:\"2\" ")
}

func TestCommandError_SafeDetails(t *testing.T) {
	req := require.New(t)
	err := NewCommandError(errors.New(testErrorMsg), 2, testErrorMsg)

	details := err.(*CommandError).SafeDetails()
	req.NotEmpty(details)
	req.Equal(string(rune(2)), details[0])
	req.Equal(testErrorMsg, details[1])
}

func TestCommandError_ExitCodeOutOfRange(t *testing.T) {
	req := require.New(t)
	err := NewCommandError(errors.New(testErrorMsg), -1, testErrorMsg)
	req.Equal(err.(*CommandError).ExitCode(), exit.GeneralError)

	err = NewCommandError(errors.New(testErrorMsg), 256, testErrorMsg)
	req.Equal(err.(*CommandError).ExitCode(), exit.GeneralError)
}

func TestCommandError_Error(t *testing.T) {
	req := require.New(t)
	zeroLenMsg := ""
	err := NewCommandError(errors.New(testErrorMsg), exit.GeneralError, zeroLenMsg)

	req.NotEmpty(err)
	req.Equal(fmt.Sprintf(commandErrorMsg, zeroLenMsg, exit.GeneralError.Int()), err.Error())
}

func TestCommandError_Is(t *testing.T) {
	req := require.New(t)
	testErrorMsg := "test errors message"
	nestedErr := NewUnsupportedOSError("Windows NT")
	cmdErr := NewCommandError(nestedErr, 1, testErrorMsg)

	req.True(errors.Is(cmdErr, &CommandError{}))

	// pass CommandError value
	req.True(errors.Is(&CommandError{cause: nestedErr, exitCode: 1, msg: testErrorMsg}, &CommandError{}))

	// pass generic error pointer
	genericErrorPtr := errors.New(testErrorMsg)
	req.False(errors.Is(genericErrorPtr, &CommandError{}))
}
