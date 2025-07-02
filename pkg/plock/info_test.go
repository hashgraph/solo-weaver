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

package plock

import (
	"github.com/stretchr/testify/require"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// probably a redundant test, but it is here to ensure implementation is not changed
func TestInfo_String(t *testing.T) {
	req := require.New(t)
	tmpDir, err := os.MkdirTemp(os.TempDir(), "plock-test")
	req.NoError(err)
	defer os.RemoveAll(tmpDir)

	helper := testHelper{}
	info := helper.createTestLock(t, 20000, tmpDir)

	activatedAt := "-"
	if info.ActivatedAt != nil {
		activatedAt = info.ActivatedAt.Format(time.RFC3339)
	}

	str := strings.Join([]string{
		info.ProviderType,
		info.Name,
		strconv.Itoa(info.PID),
		activatedAt,
		info.LockFilePath,
	}, IdentifierSeparator)

	req.Equal(str, info.String())
}
