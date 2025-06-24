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

func TestFileNotFoundError_HappyPath(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	expected := fmt.Sprintf(fileNotFoundErrorMsg, path)

	err := NewFileNotFoundError(nil, path)
	assert.NotNil(err)
	assert.NotEmpty(err)
	assert.EqualError(err, expected)

	var et *FileNotFoundError
	assert.True(errors.As(err, &et))

	details := et.SafeDetails()
	assert.Equal(path, details[0])
	assert.Equal(path, et.Path())
	assert.True(errors.Is(err, &FileNotFoundError{}))
	assert.EqualError(err, expected)
}

func TestFileNotFoundError_EmptyParameters(t *testing.T) {
	assert := require.New(t)

	path := ""
	expected := fmt.Sprintf(fileNotFoundErrorMsg, path)

	err := NewFileNotFoundError(nil, path)
	assert.NotNil(err)
	assert.NotEmpty(err)

	var et *FileNotFoundError
	assert.True(errors.As(err, &et))
	assert.Empty(et.Path())
	assert.EqualError(err, fmt.Sprintf(fileNotFoundErrorMsg, path))
	assert.Equal(expected, err.Error())
}

func TestFileNotFoundError_Cause(t *testing.T) {
	assert := require.New(t)
	err := NewFileNotFoundError(nil, "/some/path/to/file")
	assert.NotEmpty(err)
	assert.NotNil(err)

	var et *FileNotFoundError
	assert.True(errors.As(err, &et))
	assert.Empty(et.Cause())
	assert.Nil(et.Cause())
}

func TestFileNotFoundError_Unwrap(t *testing.T) {
	assert := require.New(t)
	err := NewFileNotFoundError(nil, "/some/path/to/file")
	assert.NotEmpty(err)
	assert.NotNil(err)
	var et *FileNotFoundError
	assert.True(errors.As(err, &et))
	assert.Empty(et.Unwrap())
	assert.Nil(et.Unwrap())
}

func TestFileNotFoundError_Error(t *testing.T) {
	assert := require.New(t)
	path := "/some/path/to/file"

	err := NewFileNotFoundError(nil, path)
	assert.NotNil(err)
	assert.NotEmpty(err)

	var et *FileNotFoundError
	assert.True(errors.As(err, &et))
	errMsg := et.Error()
	assert.NotEmpty(errMsg)
	assert.NotNil(errMsg)
	assert.Equal(fmt.Sprintf(fileNotFoundErrorMsg, path), errMsg)
}
