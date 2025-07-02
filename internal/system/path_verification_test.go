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
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
	"os"
	"os/user"
	"path/filepath"
	"testing"
)

type PathVerificationTestSuite struct {
	suite.Suite
	curUser *user.User
	logger  *zerolog.Logger
	pm      principal.Manager
	owner   principal.User
	group   principal.Group
	fm      fsx.Manager
	tmpRoot string
}

func (pvt *PathVerificationTestSuite) SetupSuite() {
	var err error
	req := require.New(pvt.T())

	pvt.curUser, err = user.Current()
	req.NoError(err)

	pvt.logger = logx.Nop()

	pvt.pm, err = principal.NewManager()
	req.NoError(err)
	req.NotNil(pvt.pm)

	pvt.owner, err = pvt.pm.LookupUserById(pvt.curUser.Uid)
	req.NoError(err)
	req.NotNil(pvt.owner)

	pvt.group, err = pvt.pm.LookupGroupById(pvt.curUser.Gid)
	req.NoError(err)
	req.NotNil(pvt.group)

	pvt.fm, err = fsx.NewManager(fsx.WithPrincipalManager(pvt.pm))
	req.NoError(err)
	req.NotNil(pvt.fm)

}

func (pvt *PathVerificationTestSuite) BeforeTest(suiteName, testName string) {
	for _, name := range []string{"TestPathVerifications_FilterByAction"} {
		if name == testName {
			return // skip creating temp directory tree
		}
	}

	t := pvt.T()
	req := require.New(t)
	pvt.tmpRoot = t.TempDir()

	tmpPaths := []struct {
		path     string
		mode     os.FileMode
		pathType string
	}{
		{
			filepath.Join(pvt.tmpRoot, "subdir1", "subdir2"),
			os.FileMode(0755),
			"directory",
		},
		{
			filepath.Join(pvt.tmpRoot, "subdir1", "subdir2", "subdir2_file.txt"),
			os.FileMode(0744),
			"file",
		},
		{
			filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file.txt"),
			os.FileMode(0755),
			"file",
		},
		{
			filepath.Join(pvt.tmpRoot, "file.txt"),
			os.FileMode(0755),
			"file",
		},
	}

	// create directory tree
	for _, p := range tmpPaths {
		if p.pathType == "directory" {
			err := os.MkdirAll(p.path, p.mode)
			req.NoError(err)
		} else {
			f, err := os.OpenFile(p.path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, p.mode)
			f.Close()
			req.NoError(err)
		}
	}

	// create a symlink dir
	err := os.Symlink("subdir1", filepath.Join(pvt.tmpRoot, "subdir1_link"))
	req.NoError(err)

	err = os.Symlink(filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file.txt"), filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file_link"))
	req.NoError(err)
}

func TestPathVerificationTestSuite(t *testing.T) {
	suite.Run(t, new(PathVerificationTestSuite))
}

func (pvt *PathVerificationTestSuite) TestPathVerifications_FilterByAction() {
	t := pvt.T()
	req := require.New(t)
	paths := PathVerifications{
		{
			Path: "test1",
			Actions: []PathAction{
				PathActionTypes.CreateIfMissing,
			},
		},
		{
			Path: "test2",
			Actions: []PathAction{
				PathActionTypes.IgnoreIfMissing,
			},
		},
		{
			Path: "test3",
			Actions: []PathAction{
				PathActionTypes.CreateIfMissing,
			},
		},
	}

	req.Equal(PathVerifications{paths[0], paths[2]}, paths.FilterByAction(PathActionTypes.CreateIfMissing))
	req.Equal(PathVerifications{paths[1]}, paths.FilterByAction(PathActionTypes.IgnoreIfMissing))
	req.Equal(PathVerifications{}, paths.FilterByAction(PathAction("INVALID")))
}

func (pvt *PathVerificationTestSuite) TestPathVerifications_IncorrectPermission() {
	req := require.New(pvt.T())

	var verifications = PathVerifications{
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file.txt"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
	}
	missing, err := verifications.Filter(pvt.fm, PathFilters.IncorrectPermissions)
	req.NoError(err)
	req.NotNil(missing)
	req.Equal(1, len(missing))
	req.Equal(verifications[1], missing[0])

	// check for symlink dir
	verifications = PathVerifications{
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file.txt"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1_link"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
	}

	missing, err = verifications.Filter(pvt.fm, PathFilters.IncorrectPermissions)
	req.NoError(err)
	req.NotNil(missing)
	req.Equal(1, len(missing))
	req.Equal(verifications[1], missing[0])

	// check for symlink file
	verifications = PathVerifications{
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file.txt"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file_link"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
	}

	missing, err = verifications.Filter(pvt.fm, PathFilters.IncorrectPermissions)
	req.NoError(err)
	req.Nil(missing)
	req.Equal(0, len(missing))

	// check for invalid file path
	verifications = PathVerifications{
		{
			Path:  filepath.Join(pvt.tmpRoot, "INVALID"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
		{
			Path:    filepath.Join(pvt.tmpRoot, "INVALID2"),
			Owner:   pvt.owner.Name(),
			Group:   pvt.group.Name(),
			Mode:    0755,
			Type:    "directory",
			Actions: []PathAction{PathActionTypes.IgnoreIfMissing},
		},
	}

	missing, err = verifications.Filter(pvt.fm, PathFilters.IncorrectPermissions)
	req.Error(err)
	req.True(errors.Is(err, &fsx.FileNotFoundError{}))
	var et *fsx.FileNotFoundError
	ok := errors.As(err, &et)
	req.True(ok)
	req.Equal(verifications[0].Path, et.Path())
}

func (pvt *PathVerificationTestSuite) TestPathVerifications_IncorrectOwnership() {
	req := require.New(pvt.T())

	verifications := PathVerifications{
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file.txt"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1", "subdir2"),
			Owner: "mockuser",
			Group: "mockuser",
			Mode:  0755,
			Type:  "directory",
		},
	}

	missing, err := verifications.Filter(pvt.fm, PathFilters.IncorrectOwnership)
	req.NoError(err)
	req.NotNil(missing)
	req.Equal(1, len(missing))
	req.Equal(verifications[2], missing[0])

	// check for symlink dir
	verifications = PathVerifications{
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1_link"),
			Owner: "mockuser",
			Group: "mockuser",
			Mode:  0755,
			Type:  "directory",
		},
	}

	missing, err = verifications.Filter(pvt.fm, PathFilters.IncorrectOwnership)
	req.NoError(err)
	req.NotNil(missing)
	req.Equal(verifications, missing)

	// check for symlink file
	verifications = PathVerifications{
		{
			Path:  filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file_link"),
			Owner: "mockuser",
			Group: "mockuser",
			Mode:  0755,
			Type:  "directory",
		},
	}

	missing, err = verifications.Filter(pvt.fm, PathFilters.IncorrectOwnership)
	req.NoError(err)
	req.NotNil(missing)
	req.Equal(verifications, missing)

	// check for invalid file path
	verifications = PathVerifications{
		{
			Path:  filepath.Join(pvt.tmpRoot, "INVALID"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
		{
			Path:    filepath.Join(pvt.tmpRoot, "INVALID2"),
			Owner:   pvt.owner.Name(),
			Group:   pvt.group.Name(),
			Mode:    0755,
			Type:    "directory",
			Actions: []PathAction{PathActionTypes.IgnoreIfMissing},
		},
	}

	missing, err = verifications.Filter(pvt.fm, PathFilters.IncorrectOwnership)
	req.Error(err)
	req.True(errors.Is(err, &fsx.FileNotFoundError{}))
	var et *fsx.FileNotFoundError
	ok := errors.As(err, &et)
	req.True(ok)
	req.Equal(verifications[0].Path, et.Path())
}

func (pvt *PathVerificationTestSuite) TestPathVerification_IsPathMissing() {
	req := require.New(pvt.T())

	verifications := PathVerifications{
		{
			Path: filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file.txt"),
			Type: "file",
		},
		{
			Path: filepath.Join(pvt.tmpRoot, "subdir1"),
			Type: "directory",
		},
		{
			Path: filepath.Join(pvt.tmpRoot, "subdir1", "subdir2"),
			Type: "directory",
		},
		{
			Path: filepath.Join(pvt.tmpRoot, "subdir1_link"),
			Type: "directory",
		},
		{
			Path: filepath.Join(pvt.tmpRoot, "subdir1", "subdir1_file_link"),
			Type: "file",
		},
		{
			Path: filepath.Join(pvt.tmpRoot, "subdir1", "missing_dir"),
			Type: "directory",
		},
		{
			Path: filepath.Join(pvt.tmpRoot, "subdir1", "missing_dir", "missing_file.txt"),
			Type: "file",
		},
	}

	missing, err := verifications.Filter(pvt.fm, PathFilters.IsPathMissing)
	req.NoError(err)
	req.NotNil(missing)
	req.Equal(PathVerifications{verifications[5], verifications[6]}, missing)

	// check for invalid file path
	verifications = PathVerifications{
		{
			Path:  filepath.Join(pvt.tmpRoot, "INVALID"),
			Owner: pvt.owner.Name(),
			Group: pvt.group.Name(),
			Mode:  0755,
			Type:  "directory",
		},
		{
			Path:    filepath.Join(pvt.tmpRoot, "INVALID2"),
			Owner:   pvt.owner.Name(),
			Group:   pvt.group.Name(),
			Mode:    0755,
			Type:    "directory",
			Actions: []PathAction{PathActionTypes.IgnoreIfMissing},
		},
	}

	missing, err = verifications.Filter(pvt.fm, PathFilters.IsPathMissing)
	req.NoError(err)
	req.Equal(1, len(missing))
	req.Equal(verifications[0], missing[0])
}
