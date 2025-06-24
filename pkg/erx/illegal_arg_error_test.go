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
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIllegalArgumentError_HappyPath(t *testing.T) {
	req := require.New(t)
	argName := "name"
	reason := "name argument must be a string"
	expected := fmt.Sprintf(illegalArgErrorMsg, argName, 6, reason)

	err := NewIllegalArgumentError(nil, argName, reason, 6)
	req.NotEmpty(err)
	req.EqualError(err, expected)

	req.Equal(argName, err.(*IllegalArgumentError).ArgName())
	req.Equal(reason, err.(*IllegalArgumentError).Reason())
	req.Equal(6, err.(*IllegalArgumentError).Value())

	details := err.(*IllegalArgumentError).SafeDetails()
	req.Equal(argName, details[0])
	req.Equal(reason, details[1])
}

type complexType struct {
	name  string
	value int32
}

var testComplexType = complexType{name: "complex", value: 6}

func TestIllegalArgumentError_ComplexValue(t *testing.T) {
	req := require.New(t)
	argName := "name"
	reason := "name argument must be a string"
	expected := fmt.Sprintf(illegalArgErrorMsg, argName, testComplexType, reason)

	err := NewIllegalArgumentError(nil, argName, reason, testComplexType)
	req.NotEmpty(err)
	req.Equal(expected, err.Error())

	req.Equal(argName, err.(*IllegalArgumentError).ArgName())
	req.Equal(reason, err.(*IllegalArgumentError).Reason())
	req.Equal(testComplexType, err.(*IllegalArgumentError).Value())

	// Is test
	req.True(errors.Is(err, &IllegalArgumentError{}))
}

func TestIllegalArgumentError_EmptyParameters(t *testing.T) {
	req := require.New(t)
	argName := ""
	reason := ""
	value := ""
	err := NewIllegalArgumentError(nil, argName, reason, value)
	req.NotEmpty(err)

	expected := fmt.Sprintf(illegalArgErrorMsg, argName, value, reason)
	req.Equal(expected, err.Error())
}

func TestIllegalArgumentError_Cause(t *testing.T) {
	req := require.New(t)
	err := NewIllegalArgumentError(nil, "name", "some reason", "6")
	req.NotEmpty(err)
	req.Empty(err.(*IllegalArgumentError).Cause())
}

func TestIllegalArgumentError_Unwrap(t *testing.T) {
	req := require.New(t)
	err := NewIllegalArgumentError(nil, "name", "some reason", "6")
	req.NotEmpty(err)
	req.Empty(err.(*IllegalArgumentError).Unwrap())
}

func TestIllegalArgumentError_Error(t *testing.T) {
	req := require.New(t)
	argName := "my_arg"
	reason := "some reason"
	value := "6"

	err := NewIllegalArgumentError(nil, argName, reason, value)
	req.NotEmpty(err)

	errMsg := err.(*IllegalArgumentError).Error()
	req.NotEmpty(errMsg)
	req.Equal(fmt.Sprintf(illegalArgErrorMsg, argName, value, reason), errMsg)
}
