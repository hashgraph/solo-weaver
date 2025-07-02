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
	"crypto/sha256"
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/detect"
	"golang.hedera.com/solo-provisioner/pkg/security"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
	"golang.hedera.com/solo-provisioner/pkg/software/specs"
	user2 "os/user"
	"testing"
)

const testInvalid = specs.SoftwareName("INVALID")

// test variables for this package
var (
	testSoftwareName       = specs.SoftwareName("test")
	testSoftwareHash       = fmt.Sprintf("%x", sha256.Sum256([]byte("test hash")))
	testOS                 = specs.OSType("mock-os")
	testInvalidSoftwareDef = specs.SoftwareDefinition{
		Optional: false,
		Executable: specs.SoftwareExecutableSpec{
			Name: testInvalid,
			VersionInfo: specs.VersionDetectionSpec{
				Arguments: "version",
				Regex:     "([0-9]+)+\\.([0-9]+)*\\.?([0-9]+)*[-_]?([a-zA-Z0-9\\.]+)*",
			},
			RequiredVersion: specs.VersionRequirementSpec{
				Minimum: "0.0.0",
				Maximum: "99.99.99",
			},
		},
	}
	testSoftwareDef = specs.SoftwareDefinition{
		Optional: false,
		Executable: specs.SoftwareExecutableSpec{
			Name: testSoftwareName,
			VersionInfo: specs.VersionDetectionSpec{
				Arguments: "version",
				Regex:     "([0-9]+)+\\.([0-9]+)*\\.?([0-9]+)*[-_]?([a-zA-Z0-9\\.]+)*",
			},
			RequiredVersion: specs.VersionRequirementSpec{
				Minimum: "0.0.0",
				Maximum: "99.99.99",
			},
		},
		Specs: specs.OSTypeBasedSpec{
			testOS: specs.OSFlavorBasedSpec{
				DefaultOSFlavor: specs.OSVersionBasedSpec{
					DefaultOSVersion: specs.SoftwareSpec{
						Installable:             false,
						Managed:                 false,
						DefaultVersion:          "0.0.0",
						RelaxHashVerification:   false,
						DisableHashVerification: false,
						Versions: []specs.SoftwareVersionSpec{
							{
								Version:        "0.0.0",
								PackageName:    "test",
								PackageVersion: "test",
								Sha256Hash:     testSoftwareHash,
							},
							{
								Version:        "0.0.1",
								PackageName:    "test",
								PackageVersion: "test",
								Sha256Hash:     testSoftwareHash,
							},
						},
					},
				},
			},
		},
	}
)

func TestSoftwareManager_IsMandatory(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pm := principal.NewMockManager(ctrl)
	dm := NewMockDefinitionManager(ctrl)
	manager, err := newSoftwareManager(WithDefinitionManager(dm), WithPrincipalManager(pm))

	// Explicit upcast to break at compile-time if implementation drifts
	var sm Manager = manager

	req.NoError(err)

	dm.EXPECT().GetDefinition(DockerCE).Return(specs.SoftwareDefinition{
		Optional:   false,
		Executable: specs.SoftwareExecutableSpec{},
		Specs:      specs.OSTypeBasedSpec{},
	}, nil)
	req.True(sm.IsMandatory(DockerCE))

	dm.EXPECT().GetDefinition(testInvalid).Return(
		specs.SoftwareDefinition{},
		errors.New("test error"),
	)
	req.False(sm.IsMandatory(testInvalid))
}

func TestSoftwareManager_IsAvailable(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pm := principal.NewMockManager(ctrl)
	osManager := detect.NewMockOSManager(ctrl)
	detector := NewMockProgramDetector(ctrl)
	dm := NewMockDefinitionManager(ctrl)
	sm, err := newSoftwareManager(
		WithDefinitionManager(dm),
		WithOSManager(osManager),
		WithProgramDetector(detector),
		WithPrincipalManager(pm),
	)
	req.NoError(err)

	req.False(sm.IsAvailable(testSoftwareName))

	sm.availabilityCache.Store(testSoftwareName, programInfo{})
	req.True(sm.IsAvailable(testSoftwareName))
}

func TestSoftwareManager_Verify_Version(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pm := principal.NewMockManager(ctrl)
	dm := NewMockDefinitionManager(ctrl)
	detector := NewMockProgramDetector(ctrl)
	sm, err := newSoftwareManager(
		WithProgramDetector(detector),
		WithDefinitionManager(dm),
		WithPrincipalManager(pm))
	req.NoError(err)

	progInfo := NewMockProgramInfo(ctrl)
	progInfo.EXPECT().GetPath().Return("/test").AnyTimes()

	testCases := []struct {
		requirement specs.VersionRequirementSpec
		versionSpec specs.SoftwareVersionSpec
		progVersion string
		errMsg      string
	}{
		{
			requirement: specs.VersionRequirementSpec{
				Minimum: "0.0.0",
				Maximum: "99.99.99",
			},
			versionSpec: specs.SoftwareVersionSpec{Version: "1.0.0"},
			progVersion: "1.0.0", // success
		},
		{
			requirement: specs.VersionRequirementSpec{
				Minimum: "0.0.0",
				Maximum: "99.99.99",
			},
			versionSpec: specs.SoftwareVersionSpec{Version: "1.0.0"},
			progVersion: "2.0.0", // does not match with versionSpec.Version
			errMsg:      "version mismatch",
		},
		{
			requirement: specs.VersionRequirementSpec{
				Minimum: "0.0.0",
				Maximum: "99.99.99",
			},
			versionSpec: specs.SoftwareVersionSpec{Version: ""},
			progVersion: "1.0.0", // within min and max version
			errMsg:      "",
		},
		{
			requirement: specs.VersionRequirementSpec{
				Minimum: "2.0.0",
				Maximum: "99.99.99",
			},
			versionSpec: specs.SoftwareVersionSpec{Version: ""},
			progVersion: "1.0.0",
			errMsg:      "is less than min version",
		},
		{
			requirement: specs.VersionRequirementSpec{
				Minimum: "2.0.0",
				Maximum: "3.0.0",
			},
			versionSpec: specs.SoftwareVersionSpec{Version: ""},
			progVersion: "5.0.0",
			errMsg:      "is greater than max version",
		},
		{
			requirement: specs.VersionRequirementSpec{
				Minimum: "2.x.x",
				Maximum: "3.0.0",
			},
			versionSpec: specs.SoftwareVersionSpec{Version: ""},
			progVersion: "1.0.0",
			errMsg:      "failed to parse min version",
		},
		{
			requirement: specs.VersionRequirementSpec{
				Minimum: "2.0.0",
				Maximum: "3.x.x",
			},
			versionSpec: specs.SoftwareVersionSpec{Version: ""},
			progVersion: "1.0.0",
			errMsg:      "failed to parse max version",
		},
		{
			requirement: specs.VersionRequirementSpec{
				Minimum: "2.0.0",
				Maximum: "3.0.0",
			},
			versionSpec: specs.SoftwareVersionSpec{Version: ""},
			progVersion: "1.x.x",
			errMsg:      "failed to parse program version",
		},
	}

	ctx := context.Background()
	for _, test := range testCases {
		progInfo.EXPECT().GetVersion().Return(test.progVersion)
		err = sm.verifyVersion(ctx, testSoftwareName, test.requirement, test.versionSpec, progInfo)
		if test.errMsg != "" {
			req.Error(err)
			req.Contains(err.Error(), test.errMsg)
		} else {
			req.NoError(err)
		}
	}
}

func TestSoftwareManager_CheckState_Success(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pm := principal.NewMockManager(ctrl)
	dm := NewMockDefinitionManager(ctrl)
	detector := NewMockProgramDetector(ctrl)
	detector.EXPECT().SetLogger(gomock.Any()).Return()

	osManager := detect.NewMockOSManager(ctrl)
	osManager.EXPECT().GetOSInfo().Return(detect.OSInfo{
		Type: "mock-os",
	}, nil).AnyTimes()

	manager, err := newSoftwareManager(
		WithDefinitionManager(dm),
		WithProgramDetector(detector),
		WithOSManager(osManager),
		WithPrincipalManager(pm),
	)

	// Explicit upcast to break at compile-time if implementation drifts
	var sm Manager = manager

	req.NoError(err)

	ctx := context.Background()

	// success with correct version and hash
	detector.EXPECT().GetProgramInfo(gomock.AssignableToTypeOf(ctx), testSoftwareDef.Executable).Return(&programInfo{
		path:       "/mock-path",
		mode:       0777,
		version:    "0.0.1",          // match this version
		sha256Hash: testSoftwareHash, // match this hash
	}, nil)
	req.NoError(sm.CheckState(ctx, testSoftwareDef))
	req.NoError(sm.CheckState(ctx, testSoftwareDef)) // access from cache

	req.True(sm.IsAvailable(testSoftwareName))

	// wrong definition should fail
	req.Error(sm.CheckState(ctx, testInvalidSoftwareDef))
}

func TestSoftwareManager_CheckState_Failures(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pm := principal.NewMockManager(ctrl)
	osManager := detect.NewMockOSManager(ctrl)

	dm := NewMockDefinitionManager(ctrl)
	detector := NewMockProgramDetector(ctrl)

	manager, err := newSoftwareManager(
		WithDefinitionManager(dm),
		WithProgramDetector(detector),
		WithOSManager(osManager),
		WithPrincipalManager(pm),
	)
	req.NoError(err)

	ctx := context.Background()

	// Explicit upcast to break at compile-time if implementation drifts
	var sm Manager = manager

	// fail to retrieve os specific software spec
	osManager.EXPECT().GetOSInfo().Return(detect.OSInfo{
		Type: "INVALID",
	}, nil)
	req.Error(sm.CheckState(ctx, testSoftwareDef))

	// fail to get program info
	osManager.EXPECT().GetOSInfo().Return(detect.OSInfo{
		Type: "mock-os",
	}, nil)
	detector.EXPECT().GetProgramInfo(gomock.AssignableToTypeOf(ctx), testSoftwareDef.Executable).Return(nil, errors.New("error"))
	req.Error(sm.CheckState(ctx, testSoftwareDef))

	// fail to retrieve software spec for the given program version
	osManager.EXPECT().GetOSInfo().Return(detect.OSInfo{
		Type: "mock-os",
	}, nil)
	detector.EXPECT().GetProgramInfo(gomock.AssignableToTypeOf(ctx), testSoftwareDef.Executable).Return(&programInfo{
		path:       "/mock-path",
		mode:       0777,
		version:    "INVALID", // fail for this version
		sha256Hash: testSoftwareHash,
	}, nil)
	req.Error(sm.CheckState(ctx, testSoftwareDef))

	// fail to validate explicit version
	osManager.EXPECT().GetOSInfo().Return(detect.OSInfo{
		Type: "mock-os",
	}, nil)
	detector.EXPECT().GetProgramInfo(gomock.AssignableToTypeOf(ctx), testSoftwareDef.Executable).Return(&programInfo{
		path:       "/mock-path",
		mode:       0777,
		version:    "99.0.0",         // fail for this version when comparing with the explicit default version 0.0.0
		sha256Hash: testSoftwareHash, // success for hash
	}, nil)
	req.Error(sm.CheckState(ctx, testSoftwareDef))

	// fail to retrieve software version spec for the given or default version
	testSoftwareDef.Specs[testOS][DefaultOSFlavor][DefaultOSVersion].Versions[0].Version = "1.0.0" // overwrite the default spec
	osManager.EXPECT().GetOSInfo().Return(detect.OSInfo{
		Type: "mock-os",
	}, nil)
	detector.EXPECT().GetProgramInfo(gomock.AssignableToTypeOf(ctx), testSoftwareDef.Executable).Return(&programInfo{
		path:       "/mock-path",
		mode:       0777,
		version:    "2.0.1",
		sha256Hash: testSoftwareHash, // success for hash
	}, nil)
	req.Error(sm.CheckState(ctx, testSoftwareDef))
	testSoftwareDef.Specs[testOS][DefaultOSFlavor][DefaultOSVersion].Versions[0].Version = "0.0.0" // revert

	// fail to validate hash
	osManager.EXPECT().GetOSInfo().Return(detect.OSInfo{
		Type: "mock-os",
	}, nil)
	detector.EXPECT().GetProgramInfo(gomock.AssignableToTypeOf(ctx), testSoftwareDef.Executable).Return(&programInfo{
		path:       "/mock-path",
		mode:       0777,
		version:    "0.0.1",        // success for this version
		sha256Hash: "INVALID_HASH", // fail for hash
	}, nil)
	req.Error(sm.CheckState(ctx, testSoftwareDef))
}

func TestSoftwareManager_ExecAsUser(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ctx := context.Background()

	dm := NewMockDefinitionManager(ctrl)
	pm := principal.NewMockManager(ctrl)
	manager, err := newSoftwareManager(WithDefinitionManager(dm), WithPrincipalManager(pm))
	req.NoError(err)

	// Explicit upcast to break at compile-time if implementation drifts
	var sm Manager = manager

	// fail to look up user
	pm.EXPECT().LookupUserByName(security.ServiceAccountUserName).Return(nil, nil)
	output, err := sm.Exec(ctx, "ls")
	req.Error(err)
	req.Contains(err.Error(), "failed to lookup user")

	pm.EXPECT().LookupUserByName(security.ServiceAccountUserName).Return(nil, errors.New("mock error"))
	output, err = sm.Exec(ctx, "ls")
	req.Error(err)
	req.Contains(err.Error(), "failed to lookup user")

	u, err := user2.Current()
	user := principal.NewMockUser(ctrl)
	group := principal.NewMockGroup(ctrl)
	user.EXPECT().PrimaryGroup().Return(group).AnyTimes()
	pm.EXPECT().LookupUserByName(security.ServiceAccountUserName).Return(user, nil).AnyTimes()

	user.EXPECT().Uid().Return("INVALID").Times(2)
	output, err = sm.Exec(ctx, "ls")
	req.Error(err)
	req.Contains(err.Error(), "failed to convert user ID")

	user.EXPECT().Uid().Return(u.Uid)
	group.EXPECT().Gid().Return("INVALID").Times(2)
	output, err = sm.Exec(ctx, "ls")
	req.Error(err)
	req.Contains(err.Error(), "failed to convert group ID")

	group.EXPECT().Gid().Return(u.Gid)
	user.EXPECT().Uid().Return(u.Uid)
	output, err = sm.Exec(ctx, "ls")
	req.NoError(err)
	req.NotNil(output)

	group.EXPECT().Gid().Return(u.Gid)
	user.EXPECT().Uid().Return(u.Uid)
	output, err = sm.ExecAsUser(ctx, "ls", user)
	req.NoError(err)
	req.NotNil(output)

}

func TestSoftwareManager_ScanDependencies(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx := context.Background()

	dm := NewMockDefinitionManager(ctrl)
	pm := principal.NewMockManager(ctrl)
	detector := NewMockProgramDetector(ctrl)
	detector.EXPECT().SetLogger(gomock.AssignableToTypeOf(nolog)).Return().AnyTimes()
	detector.EXPECT().GetProgramInfo(gomock.AssignableToTypeOf(ctx), testSoftwareDef.Executable).Return(&programInfo{
		path:       "/mock-path",
		mode:       0777,
		version:    "0.0.1",
		sha256Hash: testSoftwareHash,
	}, nil)
	osManager := detect.NewMockOSManager(ctrl)
	osManager.EXPECT().GetOSInfo().Return(detect.OSInfo{
		Type: testOS.String(),
	}, nil).AnyTimes()
	manager, err := newSoftwareManager(
		WithDefinitionManager(dm),
		WithPrincipalManager(pm),
		WithOSManager(osManager),
		WithProgramDetector(detector),
		WithLogger(&nolog),
	)

	// Explicit upcast to break at compile-time if implementation drifts
	var sm Manager = manager

	req.NoError(err)
	list := []specs.SoftwareDefinition{
		testSoftwareDef,
	}
	err = sm.ScanDependencies(ctx, list)
	req.NoError(err)

	// force mandatory on an invalid program
	list[0].Executable.Name = "mandatory"
	list[0].Optional = false
	detector.EXPECT().GetProgramInfo(gomock.AssignableToTypeOf(ctx), list[0].Executable).Return(nil, errors.New("mock err"))
	dm.EXPECT().GetDefinition(list[0].Executable.Name).Return(list[0], nil)
	err = sm.ScanDependencies(ctx, list)
	req.Error(err)
}
