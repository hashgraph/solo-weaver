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
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
)

func TestPermissionChangeError_HappyPath(t *testing.T) {
	assert := require.New(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	path := "/some/path/to/file"
	perms := uint(0644)
	recursive := true
	expected := fmt.Sprintf(permissionChangeErrorMsg, path, strconv.FormatUint(uint64(perms), 8), recursive)

	err := NewPermissionChangeError(nil, path, perms, recursive)
	assert.NotNil(err)
	assert.NotEmpty(err)
	assert.EqualError(err, expected)

	assert.Equal(path, err.(*PermissionChangeError).Path())
	assert.Equal(perms, err.(*PermissionChangeError).Perms())
	assert.True(err.(*PermissionChangeError).Recursive())

	details := err.(*PermissionChangeError).SafeDetails()
	assert.Equal(path, details[0])
	assert.Equal(strconv.FormatUint(uint64(perms), 8), details[1])
	assert.Equal(strconv.FormatBool(recursive), details[2])
	assert.True(errors.Is(err, err.(*PermissionChangeError)))
	assert.EqualError(err, expected)
}
func TestPermissionChangeError_EmptyParameters(t *testing.T) {
	assert := require.New(t)

	path := ""
	expected := fmt.Sprintf(permissionChangeErrorMsg, path, "0", false)

	err := NewPermissionChangeError(nil, path, 0, false)
	assert.NotNil(err)
	assert.Empty(err)
	assert.Empty(err.(*PermissionChangeError).Path())
	assert.Empty(err.(*PermissionChangeError).Perms())
	assert.False(err.(*PermissionChangeError).Recursive())

	assert.EqualError(err, expected)
	assert.Equal(expected, err.Error())

	details := err.(*PermissionChangeError).SafeDetails()
	assert.Empty(details[0])
	assert.Equal(strconv.FormatUint(0, 8), details[1])
	assert.Equal(strconv.FormatBool(false), details[2])
	assert.True(errors.Is(err, err.(*PermissionChangeError)))
	assert.EqualError(err, expected)
}

func TestPermissionsChangeError_Cause(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	perms := uint(0644)
	recursive := true

	err := NewPermissionChangeError(nil, path, perms, recursive)
	assert.NotEmpty(err)
	assert.NotNil(err)
	assert.Empty(err.(*PermissionChangeError).Cause())
	assert.Nil(err.(*PermissionChangeError).Cause())
}

func TestPermissionChangeError_Unwrap(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	perms := uint(0644)
	recursive := true

	err := NewPermissionChangeError(nil, path, perms, recursive)
	assert.NotEmpty(err)
	assert.NotNil(err)
	assert.Empty(err.(*PermissionChangeError).Unwrap())
	assert.Nil(err.(*PermissionChangeError).Unwrap())
}

func TestPermissionChangeError_Error(t *testing.T) {
	assert := require.New(t)

	path := "/some/path/to/file"
	perms := uint(0644)
	recursive := true

	err := NewPermissionChangeError(nil, path, perms, recursive)
	assert.NotNil(err)
	assert.NotEmpty(err)

	errMsg := err.(*PermissionChangeError).Error()
	assert.NotEmpty(errMsg)
	assert.NotNil(errMsg)
	assert.Equal(fmt.Sprintf(permissionChangeErrorMsg, path, strconv.FormatUint(uint64(perms), 8), recursive), errMsg)
}
