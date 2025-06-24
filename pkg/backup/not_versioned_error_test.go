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

package backup

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileAlreadyExistsError_HappyPath(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	expected := fmt.Sprintf(notVersionedErrorMsg, path)

	err := NewNotVersionedError(nil, path)
	assert.NotNil(err)
	assert.NotEmpty(err)
	assert.EqualError(err, expected)

	// assert.Equal(path, err.(*NotVersionedError).Path())
	assert.True(errors.Is(err, &NotVersionedError{}))

	var nve *NotVersionedError
	assert.True(errors.As(err, &nve))
	details := nve.SafeDetails()
	assert.Equal(path, details[0])
	assert.EqualError(err, expected)
}

func TestFileAlreadyExistsError_EmptyParameters(t *testing.T) {
	assert := require.New(t)

	path := ""
	expected := fmt.Sprintf(notVersionedErrorMsg, path)

	err := NewNotVersionedError(nil, path)
	assert.NotNil(err)
	assert.NotEmpty(err)

	var et *NotVersionedError
	assert.True(errors.As(err, &et))

	assert.Empty(et.Path())
	assert.EqualError(err, fmt.Sprintf(notVersionedErrorMsg, path))
	assert.Equal(expected, et.Error())
}

func TestFileAlreadyExistsError_Cause(t *testing.T) {
	assert := require.New(t)
	err := NewNotVersionedError(nil, "/some/path/to/file")
	assert.NotEmpty(err)
	assert.NotNil(err)

	var et *NotVersionedError
	assert.True(errors.As(err, &et))
	assert.Empty(et.Cause())
	assert.Nil(et.Cause())
}

func TestFileAlreadyExistsError_Unwrap(t *testing.T) {
	assert := require.New(t)
	err := NewNotVersionedError(nil, "/some/path/to/file")
	assert.NotEmpty(err)
	assert.NotNil(err)

	var et *NotVersionedError
	assert.True(errors.As(err, &et))
	assert.Empty(et.Unwrap())
	assert.Nil(et.Unwrap())
}

func TestFileAlreadyExistsError_Error(t *testing.T) {
	assert := require.New(t)
	path := "/some/path/to/file"

	err := NewNotVersionedError(nil, path)
	assert.NotNil(err)
	assert.NotEmpty(err)

	var et *NotVersionedError
	assert.True(errors.As(err, &et))

	errMsg := et.Error()
	assert.NotEmpty(errMsg)
	assert.NotNil(errMsg)
	assert.Equal(fmt.Sprintf(notVersionedErrorMsg, path), errMsg)
}
