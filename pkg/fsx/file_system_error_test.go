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

func TestFileSystemError_HappyPath(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	message := "some details"
	expected := fmt.Sprintf(fileSystemErrorMsg, message, path)

	err := NewFileSystemError(nil, message, path)
	assert.NotNil(err)
	assert.NotEmpty(err)
	assert.EqualError(err, expected)

	var et *FileSystemError
	assert.True(errors.As(err, &et))

	assert.Equal(path, et.Path())
	assert.Equal(message, et.Details())

	details := et.SafeDetails()
	assert.Equal(message, details[0])
	assert.Equal(path, details[1])
	assert.True(errors.Is(err, &FileSystemError{}))
	assert.EqualError(err, expected)
}

func TestFileSystemError_EmptyParameters(t *testing.T) {
	assert := require.New(t)

	path := ""
	message := ""
	expected := fmt.Sprintf(fileSystemErrorMsg, message, path)

	err := NewFileSystemError(nil, message, path)
	assert.NotNil(err)
	assert.NotEmpty(err)

	var et *FileSystemError
	assert.True(errors.As(err, &et))
	assert.Empty(et.Path())
	assert.EqualError(err, fmt.Sprintf(fileSystemErrorMsg, message, path))
	assert.Equal(expected, err.Error())
}

func TestFileSystemError_Cause(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	message := "some details"

	err := NewFileSystemError(nil, message, path)
	assert.NotEmpty(err)
	assert.NotNil(err)

	var et *FileSystemError
	assert.True(errors.As(err, &et))
	assert.Empty(et.Cause())
	assert.Nil(et.Cause())
}

func TestFileSystemError_Unwrap(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	message := "some details"

	err := NewFileSystemError(nil, message, path)
	assert.NotEmpty(err)
	assert.NotNil(err)
	var et *FileSystemError
	assert.True(errors.As(err, &et))
	assert.Empty(et.Unwrap())
	assert.Nil(et.Unwrap())
}

func TestFileSystemError_Error(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	message := "some details"

	err := NewFileSystemError(nil, message, path)
	assert.NotNil(err)
	assert.NotEmpty(err)

	var et *FileSystemError
	assert.True(errors.As(err, &et))
	errMsg := et.Error()
	assert.NotEmpty(errMsg)
	assert.NotNil(errMsg)
	assert.Equal(fmt.Sprintf(fileSystemErrorMsg, message, path), errMsg)
}
