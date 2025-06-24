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

func TestFileTypeError_HappyPath(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	fileType := Directory
	expected := fmt.Sprintf(fileTypeErrorMsg, fileType, path)

	err := NewFileTypeError(nil, fileType, path)
	assert.NotNil(err)
	assert.NotEmpty(err)
	assert.EqualError(err, expected)

	assert.Equal(path, err.(*FileTypeError).Path())
	assert.Equal(fileType, err.(*FileTypeError).ExpectedType())

	details := err.(*FileTypeError).SafeDetails()
	assert.Equal(string(fileType), details[0])
	assert.Equal(path, details[1])
	assert.True(errors.Is(err, err.(*FileTypeError)))
	assert.EqualError(err, expected)
}

func TestFileTypeError_EmptyParameters(t *testing.T) {
	assert := require.New(t)

	path := ""
	fileType := Unknown
	expected := fmt.Sprintf(fileTypeErrorMsg, fileType, path)

	err := NewFileTypeError(nil, fileType, path)
	assert.NotNil(err)
	assert.Empty(err)
	assert.Empty(err.(*FileTypeError).Path())
	assert.Empty(err.(*FileTypeError).ExpectedType())
	assert.EqualError(err, fmt.Sprintf(fileTypeErrorMsg, fileType, path))
	assert.Equal(expected, err.Error())
}

func TestFileTypeError_Cause(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	fileType := File

	err := NewFileTypeError(nil, fileType, path)
	assert.NotEmpty(err)
	assert.NotNil(err)
	assert.Empty(err.(*FileTypeError).Cause())
	assert.Nil(err.(*FileTypeError).Cause())
}

func TestFileTypeError_Unwrap(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	fileType := Symlink

	err := NewFileTypeError(nil, fileType, path)
	assert.NotEmpty(err)
	assert.NotNil(err)
	assert.Empty(err.(*FileTypeError).Unwrap())
	assert.Nil(err.(*FileTypeError).Unwrap())
}

func TestFileTypeError_Error(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	fileType := Device

	err := NewFileTypeError(nil, fileType, path)
	assert.NotNil(err)
	assert.NotEmpty(err)

	errMsg := err.(*FileTypeError).Error()
	assert.NotEmpty(errMsg)
	assert.NotNil(errMsg)
	assert.Equal(fmt.Sprintf(fileTypeErrorMsg, fileType, path), errMsg)
}
