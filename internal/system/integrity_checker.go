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
	"bytes"
	_ "embed"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/internal/models"
	"golang.hedera.com/solo-provisioner/internal/version"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"golang.hedera.com/solo-provisioner/pkg/paths"
	"golang.hedera.com/solo-provisioner/pkg/security"
	"golang.hedera.com/solo-provisioner/pkg/software"
	"golang.hedera.com/solo-provisioner/pkg/software/specs"
	"gopkg.in/yaml.v3"
	"html/template"
	"io/fs"
	"strconv"
)

//go:embed integrity.yaml
var integrityChecksStr string

type integrityCheckInfo struct {
	MandatoryFolders  []string      `yaml:"mandatory_folders" json:"mandatory_folders"`
	MandatoryFiles    []string      `yaml:"mandatory_files" json:"mandatory_files"`
	MandatoryPrograms []programInfo `yaml:"mandatory_programs" json:"mandatory_programs"`
}

type programInfo struct {
	Name                specs.SoftwareName         `yaml:"name" json:"name"`
	VersionRequirements versionRequirements        `yaml:"version" json:"version"`
	VersionDetection    specs.VersionDetectionSpec `yaml:"version_detection" json:"version_detection"`
}

type versionRequirements struct {
	Minimum       string `yaml:"minimum" json:"minimum"`
	Maximum       string `yaml:"maximum" json:"maximum"`
	GnuMinVersion string `yaml:"gnu_min" json:"gnu_min"`
	Optional      bool   `yaml:"optional" json:"optional"` // if requirement is optional
}

type OwnerInfo struct {
	UserName  string
	UserID    string
	GroupName string
	GroupID   string
}

func parseTemplatedPaths(path string, nmtPaths *paths.Paths) (string, error) {
	if nmtPaths == nil {
		return "", errors.New("NMT paths argument cannot be nil")
	}

	templateVars := map[string]string{
		models.KeyHederaAppDir:     nmtPaths.HederaAppDir.Root,
		models.KeyNodeMgmtToolsDir: nmtPaths.HederaAppDir.NodeMgmtTools.Root,
	}

	var parsed bytes.Buffer

	t, err := template.New("tmp").Parse(path)
	if err != nil {
		return "", err
	}

	if err = t.Execute(&parsed, templateVars); err != nil {
		return "", err
	}

	return parsed.String(), nil
}

func loadIntegrityCheckInfo(nmtPaths *paths.Paths) (*integrityCheckInfo, error) {
	if nmtPaths == nil {
		return nil, errors.New("NMT paths argument cannot be nil")
	}

	var checkList integrityCheckInfo
	var err error

	err = yaml.Unmarshal([]byte(integrityChecksStr), &checkList)
	if err != nil {
		return nil, err
	}

	// render templated folder paths
	var parsedPath string
	for i, p := range checkList.MandatoryFolders {
		parsedPath, err = parseTemplatedPaths(p, nmtPaths)
		if err != nil {
			return nil, errorx.IllegalArgument.New(err, "failed to parse path %q", p)
		}

		checkList.MandatoryFolders[i] = parsedPath
	}

	// render templated file paths
	for i, p := range checkList.MandatoryFiles {
		parsedPath, err = parseTemplatedPaths(p, nmtPaths)
		if err != nil {
			return nil, errorx.IllegalArgument.New(err, "failed to parse path %q", p)
		}

		checkList.MandatoryFiles[i] = parsedPath
	}

	return &checkList, nil
}

type integrityChecker struct {
	nmtPaths        *paths.Paths
	checks          *integrityCheckInfo
	fm              fsx.Manager
	logger          *zerolog.Logger
	programDetector software.ProgramDetector

	defaultOwner       OwnerInfo
	defaultFolderPerms fs.FileMode
	defaultFilePerms   fs.FileMode
}

func (ic *integrityChecker) CheckToolIntegrity() error {
	for _, folderPath := range ic.checks.MandatoryFolders {
		if !ic.fm.IsDirectory(folderPath) {
			ic.logger.Error().
				Str(logFields.path, folderPath).
				Msg("Application Integrity: Missing System Folder")

			return errors.Newf("mandatory folder is missing %q", folderPath)
		}

		ic.logger.Debug().
			Str(logFields.path, folderPath).
			Msg("Application Integrity: Verified System Folder Presence")

		err := ic.checkOwnership(folderPath)
		if err != nil {
			return err
		}

		ic.logger.Debug().
			Str(logFields.path, folderPath).
			Str(logFields.userName, ic.defaultOwner.UserName).
			Str(logFields.userID, ic.defaultOwner.UserID).
			Str(logFields.groupName, ic.defaultOwner.GroupName).
			Str(logFields.groupID, ic.defaultOwner.GroupID).
			Msg("Application Integrity: Verified System Folder Ownership")

		err = ic.checkPermissions(folderPath, ic.defaultFolderPerms)
		if err != nil {
			return err
		}

		ic.logger.Debug().
			Str(logFields.path, folderPath).
			Str(logFields.permission, strconv.FormatInt(int64(ic.defaultFolderPerms), 8)).
			Msg("Application Integrity: Verified System Folder Permissions")
	}

	for _, filePath := range ic.checks.MandatoryFiles {
		if !ic.fm.IsRegularFile(filePath) {
			ic.logger.Error().
				Str(logFields.path, filePath).
				Msg("Application Integrity: Missing System File")

			return errors.Newf("mandatory file is missing %q", filePath)
		}

		ic.logger.Debug().
			Str(logFields.path, filePath).
			Msg("Application Integrity: Verified System File Presence")

		err := ic.checkOwnership(filePath)
		if err != nil {
			return err
		}

		ic.logger.Debug().
			Str(logFields.path, filePath).
			Str(logFields.userName, ic.defaultOwner.UserName).
			Str(logFields.userID, ic.defaultOwner.UserID).
			Str(logFields.groupName, ic.defaultOwner.GroupName).
			Str(logFields.groupID, ic.defaultOwner.GroupID).
			Msg("Application Integrity: Verified System File Ownership")

		err = ic.checkPermissions(filePath, ic.defaultFilePerms)
		if err != nil {
			return err
		}

		ic.logger.Debug().
			Str(logFields.path, filePath).
			Str(logFields.permission, strconv.FormatInt(int64(ic.defaultFilePerms), 8)).
			Msg("Application Integrity: Verified System File Permissions")
	}

	return nil
}

func (ic *integrityChecker) CheckEnvironmentIntegrity() error {
	for _, prog := range ic.checks.MandatoryPrograms {
		progPath, err := ic.programDetector.DetectExecutablePath(prog.Name)
		if err != nil {
			return errorx.IllegalArgument.New(err, "failed to find path to the program %q", prog.Name)
		}

		progVersion, err := ic.programDetector.DetectProgramVersion(progPath, prog.VersionDetection)
		if err != nil {
			if !prog.VersionRequirements.Optional {
				return errorx.IllegalArgument.New(err, "failed to detect program %q version with spec %q",
					progPath, prog.VersionDetection)
			}
			continue
		}

		if progVersion == "" && !prog.VersionRequirements.Optional {
			return errors.Newf("failed to detect program %q version with spec %q",
				progPath, prog.VersionDetection)
		}

		err = version.CheckVersionRequirements(progVersion, prog.VersionRequirements.Minimum, prog.VersionRequirements.Maximum)
		if err != nil {
			//check for gnu ver
			err2 := version.CheckMinVersionRequirement(progVersion, prog.VersionRequirements.GnuMinVersion)
			if err2 != nil {
				return errorx.IllegalArgument.New(err, "program version %q failed to meet requirements %q",
					progVersion, prog.VersionRequirements)
			}
		}

		ic.logger.Info().
			Str(logFields.program, prog.Name.String()).
			Str(logFields.version, progVersion).
			Str(logFields.path, progPath).
			Msg("Prerequisite: Located Required Software")
	}

	return nil
}

func (ic *integrityChecker) checkOwnership(path string) error {
	owner, ownerGroup, err := ic.fm.ReadOwner(path)
	if err != nil {
		err = errorx.IllegalArgument.New(err, "failed to read ownership info for path %q", path)
		ic.logger.Error().
			Str(logFields.path, path).
			Err(err).
			Msg("Application Integrity: Invalid System Path Ownership")

		return err
	}

	if owner.Uid() != ic.defaultOwner.UserID || owner.Name() != ic.defaultOwner.UserName ||
		ownerGroup.Gid() != ic.defaultOwner.GroupID || ownerGroup.Name() != ic.defaultOwner.GroupName {
		ic.logger.Error().
			Str(logFields.path, path).
			Str(logFields.userID, owner.Uid()).
			Str(logFields.userName, owner.Name()).
			Str(logFields.groupID, ownerGroup.Gid()).
			Str(logFields.groupName, ownerGroup.Name()).
			Msg("Application Integrity: Invalid System Path Ownership")

		return errors.Newf("invalid system folder ownership; expected %s but found user(%s:%s) and group(%s:%s) ",
			ic.defaultOwner, owner.Name(), owner.Uid(), ownerGroup.Name(), ownerGroup.Gid())
	}

	return nil
}

func (ic *integrityChecker) checkPermissions(path string, perms fs.FileMode) error {
	fileMode, err := ic.fm.ReadPermissions(path)
	if err != nil {
		return errorx.IllegalArgument.New(err, "failed to read permission info for %q", path)
	}

	if fileMode != perms {
		ic.logger.Error().
			Str(logFields.path, path).
			Str(logFields.permission, fileMode.String()).
			Msg("Application Integrity: Invalid System Path Permissions")
		return errors.Newf("path permission mismatch; expected %s, found %s", perms.String(), fileMode.String())
	}

	return nil
}

type IntegrityCheckerOption = func(ic *integrityChecker) error

func WithIntegrityCheckerFileManager(fm fsx.Manager) IntegrityCheckerOption {
	return func(ic *integrityChecker) error {
		if fm == nil {
			return errors.New("file manager cannot be nil")
		}

		ic.fm = fm
		return nil
	}
}

func WithIntegrityCheckerLogger(logger *zerolog.Logger) IntegrityCheckerOption {
	return func(ic *integrityChecker) error {
		if logger == nil {
			return errors.New("logger cannot be nil")
		}

		ic.logger = logger
		return nil
	}
}

func WithIntegrityCheckerDefaultOwnerInfo(owner OwnerInfo) IntegrityCheckerOption {
	return func(ic *integrityChecker) error {
		ic.defaultOwner = owner
		return nil
	}
}

func WithIntegrityCheckerPathPermissions(folderPerm fs.FileMode, filePerm fs.FileMode) IntegrityCheckerOption {
	return func(ic *integrityChecker) error {
		ic.defaultFolderPerms = folderPerm
		ic.defaultFilePerms = filePerm
		return nil
	}
}

func WithProgramDetector(pd software.ProgramDetector) IntegrityCheckerOption {
	return func(ic *integrityChecker) error {
		if pd == nil {
			return errors.New("program detector cannot be nil")
		}

		ic.programDetector = pd
		return nil
	}
}

func NewIntegrityChecker(nmtPaths paths.Paths, opts ...IntegrityCheckerOption) (IntegrityChecker, error) {
	checkList, err := loadIntegrityCheckInfo(&nmtPaths)
	if err != nil {
		return nil, errorx.IllegalArgument.New(err, "failed to load integrity check list")
	}

	ic := &integrityChecker{
		nmtPaths: &nmtPaths,
		checks:   checkList,
		logger:   logx.Nop(),
		defaultOwner: OwnerInfo{
			UserName:  security.ServiceAccountUserName,
			UserID:    security.ServiceAccountUserId,
			GroupName: security.ServiceAccountGroupName,
			GroupID:   security.ServiceAccountGroupId,
		},
		defaultFolderPerms: security.ACLFolderPerms,
		defaultFilePerms:   security.ACLFilePerms,
	}

	for _, opt := range opts {
		err = opt(ic)
		if err != nil {
			return nil, err
		}
	}

	if ic.fm == nil {
		ic.fm, err = fsx.NewManager()
		if err != nil {
			return nil, err
		}
	}

	if ic.programDetector == nil {
		ic.programDetector = software.NewProgramDetector(ic.logger)
	}

	return ic, nil
}
