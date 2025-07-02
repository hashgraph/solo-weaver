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
 *
 *
 *
 */

package common

import (
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	nmtfs "golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/paths"
	"golang.hedera.com/solo-provisioner/pkg/security"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
	"golang.hedera.com/solo-provisioner/pkg/software/specs"
	"os/user"
	"testing"
)

func TestIntegrityChecker_LoadIntegrityChecks(t *testing.T) {
	req := require.New(t)
	tmpDir := "../../tmp"

	mockNmtPaths := paths.MockNmtPaths(tmpDir)
	checks, err := loadIntegrityCheckInfo(&mockNmtPaths)
	req.NoError(err)
	req.NotEmpty(checks)

	// check that templated paths are rendered
	for _, p := range checks.MandatoryFolders {
		req.NotContains(p, "{{")
	}

	// check that templated paths are rendered
	for _, p := range checks.MandatoryFiles {
		req.NotContains(p, "{{")
	}
}

func TestIntegrityChecker_CheckToolIntegrity(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	req := require.New(t)
	tmpDir := "../../tmp"

	pm, mockUser, mockGroup := setupMockPrincipalManagerWithCurrentUser(t, ctrl)
	fm, err := nmtfs.NewManager(nmtfs.WithPrincipalManager(pm))
	req.NoError(err)

	mockNmtPaths := paths.MockNmtPaths(tmpDir)
	ic, err := NewIntegrityChecker(mockNmtPaths,
		WithIntegrityCheckerFileManager(fm),
		WithIntegrityCheckerDefaultOwnerInfo(OwnerInfo{
			UserName:  mockUser.Name(),
			UserID:    mockUser.Uid(),
			GroupName: mockGroup.Name(),
			GroupID:   mockGroup.Gid(),
		}),
	)
	req.NoError(err)
	req.NotNil(ic)

	req.NoError(ic.CheckToolIntegrity())
}

func TestIntegrityChecker_CheckEnvironmentIntegrity(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	req := require.New(t)
	tmpDir := "../../tmp"

	pm, mockUser, mockGroup := setupMockPrincipalManagerWithCurrentUser(t, ctrl)
	fm, err := nmtfs.NewManager(nmtfs.WithPrincipalManager(pm))
	req.NoError(err)

	mockNmtPaths := paths.MockNmtPaths(tmpDir)
	ic, err := NewIntegrityChecker(mockNmtPaths,
		WithIntegrityCheckerFileManager(fm),
		WithIntegrityCheckerDefaultOwnerInfo(OwnerInfo{
			UserName:  mockUser.Name(),
			UserID:    mockUser.Uid(),
			GroupName: mockGroup.Name(),
			GroupID:   mockGroup.Gid(),
		}),
	)
	req.NoError(err)
	req.NotNil(ic)

	ic.(*integrityChecker).checks.MandatoryPrograms = []programInfo{
		{
			Name: "rsync",
			VersionRequirements: versionRequirements{
				Minimum: "3.0.0",
			},
			VersionDetection: specs.VersionDetectionSpec{
				Arguments: "--version",
				Regex:     "[0-9]+.[0-9]{1,2}",
			},
		},
		{
			Name: "ls",
			VersionRequirements: versionRequirements{
				Minimum:  "8.00",
				Optional: true,
			},
			VersionDetection: specs.VersionDetectionSpec{
				Arguments: "--version",
				Regex:     "[0-9]+.[0-9]{1,2}",
			},
		},
	}
	req.NoError(ic.CheckEnvironmentIntegrity())
}

func setupMockPrincipalManagerWithCurrentUser(t *testing.T, ctrl *gomock.Controller) (principal.Manager, principal.User, principal.Group) {
	req := require.New(t)

	curUser, err := user.Current()
	req.NoError(err)
	curGroup, err := user.LookupGroupId(curUser.Gid)
	req.NoError(err)

	return setupMockPrincipalManager(ctrl, curUser, curGroup)
}

func setupMockPrincipalManager(ctrl *gomock.Controller, curUser *user.User, curGroup *user.Group) (principal.Manager, principal.User, principal.Group) {
	pm := principal.NewMockManager(ctrl)

	mockUser := principal.NewMockUser(ctrl)
	mockUser.EXPECT().Uid().Return(curUser.Uid).AnyTimes()
	mockUser.EXPECT().Name().Return(curUser.Username).AnyTimes()

	mockGroup := principal.NewMockGroup(ctrl)
	mockGroup.EXPECT().Gid().Return(curGroup.Gid).AnyTimes()
	mockGroup.EXPECT().Name().Return(curGroup.Name).AnyTimes()

	mockUser.EXPECT().PrimaryGroup().Return(mockGroup).AnyTimes()

	pm.EXPECT().LookupUserById(curUser.Uid).Return(mockUser, nil).AnyTimes()
	pm.EXPECT().LookupGroupById(curUser.Gid).Return(mockGroup, nil).AnyTimes()

	pm.EXPECT().LookupUserById(security.ServiceAccountUserId).Return(mockUser, nil).AnyTimes()
	pm.EXPECT().LookupUserByName(security.ServiceAccountUserName).Return(mockUser, nil).AnyTimes()
	pm.EXPECT().LookupGroupById(security.ServiceAccountGroupId).Return(mockGroup, nil).AnyTimes()
	pm.EXPECT().LookupGroupByName(security.ServiceAccountGroupName).Return(mockGroup, nil).AnyTimes()

	return pm, mockUser, mockGroup
}
