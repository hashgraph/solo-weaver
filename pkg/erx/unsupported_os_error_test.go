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

func TestUnsupportedOSError_HappyPath(t *testing.T) {
	req := require.New(t)

	osName := "Windows NT"
	expected := fmt.Sprintf(UnsupportedOSErrorMsg, osName)

	err := NewUnsupportedOSError(osName)
	req.NotEmpty(err)
	req.Equal(expected, err.Error())
	req.Equal(osName, err.(*UnsupportedOSError).Name())
	req.Equal(osName, err.(*UnsupportedOSError).SafeDetails()[0])
	req.True(errors.Is(err, &UnsupportedOSError{}))
}

func TestUnsupportedOSError_EmptyName(t *testing.T) {
	req := require.New(t)
	err := NewUnsupportedOSError("")
	req.Empty(err)
}

func TestUnsupportedOSError_Cause(t *testing.T) {
	req := require.New(t)
	osName := "Windows NT"

	err := NewUnsupportedOSError(osName)
	req.NotEmpty(err)
	req.Empty(err.(*UnsupportedOSError).Cause())
}

func TestUnsupportedOSError_Unwrap(t *testing.T) {
	req := require.New(t)
	osName := "Windows NT"

	err := NewUnsupportedOSError(osName)
	req.NotEmpty(err)
	req.Empty(err.(*UnsupportedOSError).Unwrap())
}
