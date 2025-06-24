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

package fsx

import (
	"errors"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestFileAlreadyExistsError_HappyPath(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	expected := fmt.Sprintf(fileAlreadyExistsErrorMsg, path)

	err := NewFileAlreadyExistsError(nil, path)
	assert.NotNil(err)
	assert.NotEmpty(err)
	assert.EqualError(err, expected)

	assert.Equal(path, err.(*FileAlreadyExistsError).Path())

	details := err.(*FileAlreadyExistsError).SafeDetails()
	assert.Equal(path, details[0])
	assert.True(errors.Is(err, err.(*FileAlreadyExistsError)))
	assert.EqualError(err, expected)
}

func TestFileAlreadyExistsError_EmptyParameters(t *testing.T) {
	assert := require.New(t)

	path := ""
	expected := fmt.Sprintf(fileAlreadyExistsErrorMsg, path)

	err := NewFileAlreadyExistsError(nil, path)
	assert.NotNil(err)
	assert.Empty(err)
	assert.Empty(err.(*FileAlreadyExistsError).Path())
	assert.EqualError(err, fmt.Sprintf(fileAlreadyExistsErrorMsg, path))
	assert.Equal(expected, err.Error())
}

func TestFileAlreadyExistsError_Cause(t *testing.T) {
	assert := require.New(t)
	err := NewFileAlreadyExistsError(nil, "/some/path/to/file")
	assert.NotEmpty(err)
	assert.NotNil(err)
	assert.Empty(err.(*FileAlreadyExistsError).Cause())
	assert.Nil(err.(*FileAlreadyExistsError).Cause())
}

func TestFileAlreadyExistsError_Unwrap(t *testing.T) {
	assert := require.New(t)
	err := NewFileAlreadyExistsError(nil, "/some/path/to/file")
	assert.NotEmpty(err)
	assert.NotNil(err)
	assert.Empty(err.(*FileAlreadyExistsError).Unwrap())
	assert.Nil(err.(*FileAlreadyExistsError).Unwrap())
}

func TestFileAlreadyExistsError_Error(t *testing.T) {
	assert := require.New(t)
	path := "/some/path/to/file"

	err := NewFileAlreadyExistsError(nil, path)
	assert.NotNil(err)
	assert.NotEmpty(err)

	errMsg := err.(*FileAlreadyExistsError).Error()
	assert.NotEmpty(errMsg)
	assert.NotNil(errMsg)
	assert.Equal(fmt.Sprintf(fileAlreadyExistsErrorMsg, path), errMsg)
}
