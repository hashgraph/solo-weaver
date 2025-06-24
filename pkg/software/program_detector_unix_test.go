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

package software

import (
	"context"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/software/specs"
	"testing"
)

func TestUnixDetector_GetInfo(t *testing.T) {
	req := require.New(t)
	ud := NewUnixProgramDetector(&nolog)

	var testCases = []struct {
		name            specs.SoftwareName
		defaultLocation string
		versionRegex    string
		errMsg          string
	}{
		{
			name:            specs.SoftwareName("rsync"),
			versionRegex:    "([0-9]+)+\\.([0-9]+)*\\.?([0-9]+)*[-_]?([a-zA-Z0-9\\.]+)*",
			defaultLocation: "/usr/local/bin/rsync",
			errMsg:          "",
		},
		{
			name:            specs.SoftwareName("rsync"),
			versionRegex:    "([0-9]+)+\\.([0-9]+)*\\.?([0-9]+)*[-_]?([a-zA-Z0-9\\.]+)*",
			defaultLocation: "/usr/bin/rsync",
			errMsg:          "",
		},
		{
			name:            specs.SoftwareName("rsync"),
			versionRegex:    "([0-9]+)+\\.([0-9]+)*\\.?([0-9]+)*[-_]?([a-zA-Z0-9\\.]+)*",
			defaultLocation: "/invalid", // invalid path should resolve into correct path
			errMsg:          "",
		},
		{
			name:            specs.SoftwareName("rsync"),
			versionRegex:    "([0-9]+)+\\.([0-9]+)*\\.?([0-9]+)*[-_]?([a-zA-Z0-9\\.]+)*",
			defaultLocation: "", // empty path should force it to resolve to the correct path
			errMsg:          "",
		},
		{
			name:            specs.SoftwareName("INVALID"),
			versionRegex:    "([0-9]+)+\\.([0-9]+)*\\.?([0-9]+)*[-_]?([a-zA-Z0-9\\.]+)*",
			defaultLocation: "",
			errMsg:          "failed to find path to the program",
		},
		{
			name:            specs.SoftwareName("INVALID"),
			versionRegex:    "([0-9]+)+\\.([0-9]+)*\\.?([0-9]+)*[-_]?([a-zA-Z0-9\\.]+)*",
			defaultLocation: "/invalid",
			errMsg:          "failed to find path to the program",
		},
		{
			name:            specs.SoftwareName("rsync"),
			versionRegex:    "([", // invalid regex should fail
			defaultLocation: "",   // empty path should force it to resolve to the correct path
			errMsg:          "failed to parse version regex",
		},
	}

	for _, test := range testCases {
		execInfo := specs.SoftwareExecutableSpec{
			Name:            test.name,
			DefaultLocation: test.defaultLocation,
			VersionInfo: specs.VersionDetectionSpec{
				Arguments: "--version",
				Regex:     test.versionRegex,
			},
			RequiredVersion: specs.VersionRequirementSpec{},
		}

		ctx := context.Background()
		info, err := ud.GetProgramInfo(ctx, execInfo)
		if test.errMsg != "" {
			req.Error(err)
			req.Contains(err.Error(), test.errMsg)
			req.Nil(info)
		} else {
			req.NoError(err)
			req.NotEmpty(info.GetPath())
			req.NotEmpty(info.GetVersion())
			req.NotEmpty(info.GetHash())
			req.NotEmpty(info.GetFileMode())
			req.True(info.IsExecAll())
		}
	}
}
