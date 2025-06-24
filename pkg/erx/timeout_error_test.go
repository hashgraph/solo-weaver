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

func TestTimeoutError_HappyPath(t *testing.T) {
	req := require.New(t)
	operationName := "Open File"
	expected := fmt.Sprintf(timeoutErrorMsg, operationName)

	err := NewTimeoutError(operationName)
	req.NotEmpty(err)
	req.Equal(expected, err.Error())
	req.Equal(operationName, err.(*TimeoutError).Name())
	req.Equal(operationName, err.(*TimeoutError).SafeDetails()[0])
}

func TestTimeoutError_Is(t *testing.T) {
	req := require.New(t)
	operationName := "Open File"
	err := NewTimeoutError(operationName)
	req.True(errors.Is(err, &TimeoutError{}))
}

func TestTimeoutError_EmptyName(t *testing.T) {
	req := require.New(t)
	err := NewTimeoutError("")
	req.Empty(err)
}

func TestTimeoutError_Cause(t *testing.T) {
	req := require.New(t)
	operationName := "Open File"

	err := NewTimeoutError(operationName)
	req.NotEmpty(err)
	req.Empty(err.(*TimeoutError).Cause())
}

func TestTimeoutError_Unwrap(t *testing.T) {
	req := require.New(t)
	operationName := "Open File"

	err := NewTimeoutError(operationName)
	req.NotEmpty(err)
	req.Empty(err.(*TimeoutError).Unwrap())
}
