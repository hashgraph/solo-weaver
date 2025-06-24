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
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
	"strconv"
	"testing"
)

func TestOwnershipChangeError_HappyPath(t *testing.T) {
	assert := require.New(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	path := "/some/path/to/file"
	user := principal.NewMockUser(ctrl)
	group := principal.NewMockGroup(ctrl)

	user.EXPECT().Name().Return("user").AnyTimes()
	group.EXPECT().Name().Return("group").AnyTimes()

	recursive := true
	expected := fmt.Sprintf(ownershipChangeErrorMsg, path, user.Name(), group.Name(), recursive)

	err := NewOwnershipChangeError(nil, path, user, group, recursive)
	assert.NotNil(err)
	assert.NotEmpty(err)
	assert.EqualError(err, expected)

	assert.Equal(path, err.(*OwnershipChangeError).Path())
	assert.Equal(user, err.(*OwnershipChangeError).User())
	assert.Equal(group, err.(*OwnershipChangeError).Group())
	assert.True(err.(*OwnershipChangeError).Recursive())

	details := err.(*OwnershipChangeError).SafeDetails()
	assert.Equal(path, details[0])
	assert.Equal("user", details[1])
	assert.Equal("group", details[2])
	assert.Equal(strconv.FormatBool(recursive), details[3])
	assert.True(errors.Is(err, err.(*OwnershipChangeError)))
	assert.EqualError(err, expected)
}

func TestOwnershipChangeError_EmptyParameters(t *testing.T) {
	assert := require.New(t)

	path := ""
	expected := fmt.Sprintf(ownershipChangeErrorMsg, path, "", "", false)

	err := NewOwnershipChangeError(nil, path, nil, nil, false)
	assert.NotNil(err)
	assert.Empty(err)
	assert.Empty(err.(*OwnershipChangeError).Path())
	assert.Empty(err.(*OwnershipChangeError).User())
	assert.Empty(err.(*OwnershipChangeError).Group())
	assert.False(err.(*OwnershipChangeError).Recursive())

	assert.EqualError(err, expected)
	assert.Equal(expected, err.Error())

	details := err.(*OwnershipChangeError).SafeDetails()
	assert.Empty(details[0])
	assert.Empty(details[1])
	assert.Empty(details[2])
	assert.Equal(strconv.FormatBool(false), details[3])
	assert.True(errors.Is(err, err.(*OwnershipChangeError)))
	assert.EqualError(err, expected)
}

func TestOwnershipChangeError_Cause(t *testing.T) {
	assert := require.New(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	path := "/some/path/to/file"
	user := principal.NewMockUser(ctrl)
	group := principal.NewMockGroup(ctrl)

	user.EXPECT().Name().Return("user").AnyTimes()
	group.EXPECT().Name().Return("group").AnyTimes()

	err := NewOwnershipChangeError(nil, path, user, group, false)
	assert.NotEmpty(err)
	assert.NotNil(err)
	assert.Empty(err.(*OwnershipChangeError).Cause())
	assert.Nil(err.(*OwnershipChangeError).Cause())
}

func TestOwnershipChangeError_Unwrap(t *testing.T) {
	assert := require.New(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	path := "/some/path/to/file"
	user := principal.NewMockUser(ctrl)
	group := principal.NewMockGroup(ctrl)

	user.EXPECT().Name().Return("user").AnyTimes()
	group.EXPECT().Name().Return("group").AnyTimes()

	err := NewOwnershipChangeError(nil, path, user, group, false)
	assert.NotEmpty(err)
	assert.NotNil(err)
	assert.Empty(err.(*OwnershipChangeError).Unwrap())
	assert.Nil(err.(*OwnershipChangeError).Unwrap())
}

func TestOwnershipChangeError_Error(t *testing.T) {
	assert := require.New(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	path := "/some/path/to/file"
	user := principal.NewMockUser(ctrl)
	group := principal.NewMockGroup(ctrl)

	user.EXPECT().Name().Return("user").AnyTimes()
	group.EXPECT().Name().Return("group").AnyTimes()

	err := NewOwnershipChangeError(nil, path, user, group, false)
	assert.NotNil(err)
	assert.NotEmpty(err)

	errMsg := err.(*OwnershipChangeError).Error()
	assert.NotEmpty(errMsg)
	assert.NotNil(errMsg)
	assert.Equal(fmt.Sprintf(ownershipChangeErrorMsg, path, user.Name(), group.Name(), false), errMsg)
}
