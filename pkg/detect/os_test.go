/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
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

package detect

import (
	"github.com/cockroachdb/errors"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"runtime"
	"testing"
)

func TestOsManager_GetOSInfo(t *testing.T) {
	req := require.New(t)

	om := NewOSManager(WithOSManagerLogger(nolog))
	osInfo, err := om.GetOSInfo()
	req.NoError(err)
	req.NotEmpty(osInfo.Type)
	req.NotEmpty(osInfo.Architecture)
	req.Equal(runtime.GOOS, osInfo.Type)
	req.Equal(runtime.GOARCH, osInfo.Architecture)
}

func TestOsManager_GetOSInfo_Fail(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	d := NewMockOSDetector(ctrl)
	om := NewOSManager(WithOSDetector(d), WithOSManagerLogger(nolog))
	d.EXPECT().ScanOS().Return(OSInfo{}, errors.New("error"))
	_, err := om.GetOSInfo()
	req.Error(err)
}
