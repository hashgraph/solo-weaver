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

package paths

import (
	"context"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"os"
	"path/filepath"
	"strings"
)

// Scout is a path discoverer for NMT
type Scout struct {
	workDir        string // allow scout to have the PWD to be set beforehand which also helps with unit testing
	directoryNames *DirectoryNames
	logger         *zerolog.Logger
}

// ScoutBuilder implements builder pattern for Scout
type ScoutBuilder struct {
	ctx            context.Context
	workDir        string
	directoryNames *DirectoryNames
	logger         *zerolog.Logger
}

// NewScoutBuilder returns scout builder with default settings
func NewScoutBuilder(ctx context.Context, logger *zerolog.Logger) *ScoutBuilder {
	return &ScoutBuilder{
		ctx:     ctx,
		workDir: "", // empty will force it to determine its current working directory
		logger:  logger,
		directoryNames: &DirectoryNames{
			ConfigDirName:      ConfigDirName,
			LogDirName:         LogsDirName,
			HgcAppDirName:      HederaAppDirName,
			NodeMgmtDirName:    NodeMgmtDirName,
			SkippedFolderNames: defaultSkippedFolderNames,
		},
	}
}

// Build returns an instance of scout
func (sb *ScoutBuilder) Build() *Scout {
	return &Scout{
		workDir:        sb.workDir,
		directoryNames: sb.directoryNames,
		logger:         sb.logger,
	}
}

// SetWorkDir sets work directory for the scout
// An empty string will force Scout to determine working directory of the program
func (sb *ScoutBuilder) SetWorkDir(workDir string) *ScoutBuilder {
	sb.workDir = workDir
	return sb
}

// SetConfigDirName sets the name of the config directory
func (sb *ScoutBuilder) SetConfigDirName(dirName string) *ScoutBuilder {
	sb.directoryNames.ConfigDirName = dirName
	return sb
}

// SetLogDirName sets the name of the log directory
func (sb *ScoutBuilder) SetLogDirName(dirName string) *ScoutBuilder {
	sb.directoryNames.LogDirName = dirName
	return sb
}

// SetHgcAppDirName sets the name of the hgcapp directory which is the root of the hedera node app
func (sb *ScoutBuilder) SetHgcAppDirName(dirName string) *ScoutBuilder {
	sb.directoryNames.HgcAppDirName = dirName
	return sb
}

// SetNodeMgmtToolsDirName sets the name of the solo-provisioner directory
func (sb *ScoutBuilder) SetNodeMgmtToolsDirName(dirName string) *ScoutBuilder {
	sb.directoryNames.NodeMgmtDirName = dirName
	return sb
}

// SetSkipFolderNames sets the name of the folders to skip during path discovery o
func (sb *ScoutBuilder) SetSkipFolderNames(skip []string) *ScoutBuilder {
	sb.directoryNames.SkippedFolderNames = skip
	return sb
}

// Discover uses the scout to discover relevant paths
func (s *Scout) Discover(ctx context.Context, createIfNotFound bool) (*Paths, error) {
	var err error
	paths := &Paths{}

	if s.workDir == "" {
		s.workDir, err = s.findWorkDir()
		if err != nil {
			return nil, err
		}
	}

	paths.WorkDir = s.workDir
	s.logger.Debug().
		Str("WorkDir", s.workDir).
		Msg("Found work dir")

	paths.AppDir, err = s.findAppDir()
	if err != nil {
		return nil, err
	}
	s.logger.Debug().
		Str("AppDir", paths.AppDir).
		Msg("Found application dir")

	paths.LogDir, err = s.findLogFolder(createIfNotFound)
	if err != nil && createIfNotFound {
		return nil, err
	}
	s.logger.Debug().
		Str("LogDir", paths.LogDir).
		Msg("Found log dir")

	paths.ConfigDir, err = s.findConfigFolder(createIfNotFound)
	if err != nil && createIfNotFound {
		return nil, err
	}
	s.logger.Debug().
		Str("Config", paths.ConfigDir).
		Msg("Found config dir")

	paths.HederaAppDir, err = s.discoverHederaAppDir(s.workDir, createIfNotFound)
	if err != nil {
		return nil, err
	}

	return paths, nil
}

// discoverHederaAppDir checks for Hedera App dir in the parent path from the current work directory
func (s *Scout) discoverHederaAppDir(curDir string, createIfNotFound bool) (*HederaAppDir, error) {
	var err error
	hgcappDir := &HederaAppDir{}

	hgcappDir.Root, err = FindParentPath(curDir, s.directoryNames.HgcAppDirName)
	if err != nil {
		if createIfNotFound {
			parentRoot, _ := filepath.Split(curDir)
			parentPath := filepath.Join(parentRoot, s.directoryNames.HgcAppDirName)
			s.logger.Debug().
				Str("HgAppDir", parentPath).
				Msg("Creating Hedera App Dir")
			if err := makeDir(parentPath); err != nil {
				return nil, err
			}
			hgcappDir.Root = parentPath
		} else {
			return nil, err
		}
	}
	s.logger.Debug().
		Str("HgAppDir", hgcappDir.Root).
		Msg("Found Hedera App Dir")

	hgcappDir.UploaderMirror = filepath.Join(hgcappDir.Root, UploaderMirrorDirName)
	hgcappDir.HederaBackups = filepath.Join(hgcappDir.Root, HederaBackupsDirName)

	hgcappDir.NodeMgmtTools, err = s.discoverNodeMgmtDir(hgcappDir.Root, createIfNotFound)
	if err != nil {
		return nil, err
	}

	hgcappDir.HederaServices, err = s.discoverHederaServicesDir(hgcappDir.Root, createIfNotFound)
	if err != nil {
		return nil, err
	}

	return hgcappDir, nil

}

// discoverNodeMgmtDir checks for the "Node Management Tools" directory from the given root directory
func (s *Scout) discoverNodeMgmtDir(rootDir string, createIfNotFound bool) (*NodeMgmtToolsDir, error) {
	nmtRootDir := filepath.Join(rootDir, NodeMgmtDirName)
	nmtBinDir := filepath.Join(nmtRootDir, "bin")
	nmtCommonDir := filepath.Join(nmtRootDir, "common")
	nmtConfigDir := filepath.Join(nmtRootDir, "config")
	nmtImagesDir := filepath.Join(nmtRootDir, "images")
	nmtLogsDir := filepath.Join(nmtRootDir, "logs")
	nmtStateDir := filepath.Join(nmtRootDir, "state")

	status := s.checkDirectoryPaths([]string{
		nmtRootDir,
		nmtBinDir,
		nmtCommonDir,
		nmtConfigDir,
		nmtImagesDir,
		nmtLogsDir,
		nmtStateDir,
	}, createIfNotFound)

	// check for status errors
	for _, err := range status {
		if err != nil {
			return nil, err
		}
	}

	upgradeDir, err := s.discoverUpgradeDir(nmtRootDir, createIfNotFound)
	if err != nil {
		return nil, err
	}

	nmtComposeDir, err := s.discoverComposeDir(nmtRootDir, createIfNotFound)
	if err != nil {
		return nil, err
	}

	nmtDir := &NodeMgmtToolsDir{
		Root:    nmtRootDir,
		Bin:     nmtBinDir,
		Common:  nmtCommonDir,
		Compose: nmtComposeDir,
		Config:  nmtConfigDir,
		Image:   nmtImagesDir,
		Logs:    nmtLogsDir,
		State:   nmtStateDir,
		Upgrade: upgradeDir,
	}
	s.logger.Debug().
		Any("NMT Dirs", nmtDir).
		Msg("Found NMT Dirs")

	return nmtDir, nil
}

// discoverUpgradeDir checks for the upgrade directory in the children paths from the given root directory
func (s *Scout) discoverUpgradeDir(rootDir string, createIfNotFound bool) (*UpgradeDir, error) {
	upgradeRootDir := filepath.Join(rootDir, UpgradeDirName)
	err := s.checkDirectoryPath(upgradeRootDir, createIfNotFound)
	if err != nil {
		return nil, err
	}

	// check for child paths
	currentDir := filepath.Join(upgradeRootDir, "current")
	previousDir := filepath.Join(upgradeRootDir, "previous")
	pendingDir := filepath.Join(upgradeRootDir, "pending")
	status := s.checkDirectoryPaths([]string{
		currentDir,
		previousDir,
		pendingDir,
	}, createIfNotFound)

	for _, err := range status {
		if err != nil {
			return nil, err
		}
	}

	upgradeDir := &UpgradeDir{
		Root:     upgradeRootDir,
		Current:  currentDir,
		Pending:  pendingDir,
		Previous: previousDir,
	}
	s.logger.Debug().
		Str("root Dir", rootDir).
		Any("Upgrade Dirs", upgradeDir).
		Msg("Found Upgrade Dirs")

	return upgradeDir, nil
}

func (s *Scout) discoverComposeDir(rootDir string, createIfNotFound bool) (*ComposeDir, error) {
	composeRootDir := filepath.Join(rootDir, ComposeDirName)
	if err := s.checkDirectoryPath(composeRootDir, createIfNotFound); err != nil {
		return nil, err
	}

	networkNodeDir := filepath.Join(composeRootDir, "network-node")
	status := s.checkDirectoryPaths([]string{networkNodeDir}, createIfNotFound)

	for _, err := range status {
		if err != nil {
			return nil, err
		}
	}

	composeDir := &ComposeDir{
		Root:        composeRootDir,
		NetworkNode: networkNodeDir,
	}

	s.logger.Debug().
		Str("root Dir", rootDir).
		Any("Upgrade Dirs", composeDir).
		Msg("Found Compose Dirs")

	return composeDir, nil
}

// discoverHederaServicesDir checks for the "Hedera Services" directory in the children from the given root directory
func (s *Scout) discoverHederaServicesDir(rootDir string, createIfNotFound bool) (*HederaServicesDir, error) {
	svcRootDir := filepath.Join(rootDir, HederaServicesDirName)
	err := s.checkDirectoryPath(svcRootDir, createIfNotFound)
	if err != nil {
		return nil, err
	}

	hapiApiDir, err := s.discoverHapiAppDir(svcRootDir, createIfNotFound)
	if err != nil {
		return nil, err
	}

	svcDir := &HederaServicesDir{
		Root:    svcRootDir,
		HapiApp: hapiApiDir,
	}

	s.logger.Debug().
		Str("root Dir", rootDir).
		Any("Services Dirs", svcDir).
		Msg("Found Hedera Services Dirs")

	return svcDir, nil
}

// discoverHapiAppDir checks for the "HapiApp" directory in the children from the given root directory
func (s *Scout) discoverHapiAppDir(svcRootDir string, createIfNotFound bool) (*HapiAppDir, error) {
	hapiAppRootDir := filepath.Join(svcRootDir, HederaApiDirName)
	err := s.checkDirectoryPath(hapiAppRootDir, createIfNotFound)
	if err != nil {
		return nil, err
	}

	hapiDataDir, err := s.discoverHapiAppDataDir(hapiAppRootDir, createIfNotFound)
	if err != nil {
		return nil, err
	}

	hapiAppDir := &HapiAppDir{
		Root: hapiAppRootDir,
		Data: hapiDataDir,
	}

	s.logger.Debug().
		Str("root Dir", svcRootDir).
		Any("Hedera API App Dirs", hapiAppDir).
		Msg("Found Hedera API App Dirs")

	return hapiAppDir, nil
}

// discoverHapiAppDataDir checks for the data directory in the children from the given root directory
func (s *Scout) discoverHapiAppDataDir(rootDir string, createIfNotFound bool) (*HapiAppDataDir, error) {
	dataRootDir := filepath.Join(rootDir, DataDirName)
	err := s.checkDirectoryPath(dataRootDir, createIfNotFound)
	if err != nil {
		return nil, err
	}

	// check for children paths
	configDir := filepath.Join(dataRootDir, "config")
	diskFsDir := filepath.Join(dataRootDir, "diskFs")
	keysDir := filepath.Join(dataRootDir, "keys")
	onboardDir := filepath.Join(dataRootDir, "onboard")
	savedDir := filepath.Join(dataRootDir, "saved")
	statsDir := filepath.Join(dataRootDir, "stats")
	status := s.checkDirectoryPaths([]string{
		configDir,
		diskFsDir,
		keysDir,
		onboardDir,
		savedDir,
		statsDir,
	}, createIfNotFound)
	for _, err := range status {
		if err != nil {
			return nil, err
		}

	}

	upgradeDir, err := s.discoverUpgradeDir(dataRootDir, createIfNotFound)
	if err != nil {
		return nil, err
	}

	dataDir := &HapiAppDataDir{
		Root:    dataRootDir,
		Config:  configDir,
		DiskFs:  diskFsDir,
		Keys:    keysDir,
		OnBoard: onboardDir,
		Saved:   savedDir,
		Stats:   statsDir,
		Upgrade: upgradeDir,
	}

	s.logger.Debug().
		Str("root Dir", rootDir).
		Any("Hedera API App Data Dirs", dataDir).
		Msg("Found Hedera API App Data Dirs")

	return dataDir, nil
}

// findWorkDir returns the absolute path of the directory which contains the currently executing program.
func (s *Scout) findWorkDir() (string, error) {
	exFile, err := os.Executable()
	if err != nil {
		return "", errors.Wrap(err, "failed to locate the path to the current program")
	}

	absPath, err := filepath.Abs(exFile)
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve the absolute path to the current program")
	}

	return filepath.Dir(absPath), nil
}

// findExecutableName returns the filename of the currently executing program without any file extensions.
func (s *Scout) findExecutableName() string {
	name := filepath.Base(os.Args[0])

	if idx := strings.Index(name, "."); idx >= 0 {
		return name[0:idx]
	}

	return name
}

// findAppDir attempts to locate the application directory by walking up from the WorkDir directory
// skipping specified directories such as 'bin' or 'local' and stopping at the first directory not matching the
// skipped folder names.
func (s *Scout) findAppDir() (string, error) {
	var err error
	exFolder := s.workDir
	if exFolder == "" {
		exFolder, err = s.findWorkDir()
		if err != nil {
			exFolder = filepath.Dir(os.Args[0])

			if exFolder == "" {
				return "", errors.Wrap(err, "all attempts to resolve the executable location have failed")
			}
		}
	}

	finalPath, err := TrimFromPath(exFolder, s.directoryNames.SkippedFolderNames)
	return finalPath, errors.Wrap(err, "unable to locate the application folder")
}

// findConfigFolder returns the absolute path to the application config folder
// optionally creating the folder if it does not exist.
func (s *Scout) findConfigFolder(createIfNotFound bool) (string, error) {
	return ResolveFolder(s.findAppDir, s.directoryNames.ConfigDirName, createIfNotFound)
}

// findLogFolder returns the absolute path to the application log folder
// optionally creating the folder if it does not exist.
func (s *Scout) findLogFolder(createIfNotFound bool) (string, error) {
	return ResolveFolder(s.findAppDir, s.directoryNames.LogDirName, createIfNotFound)
}

// checkDirectoryPath checks for the existence of a directory at the given path or creates it if required
// It returns error if the path does not exist and does not need to be created
func (s *Scout) checkDirectoryPath(fullPath string, createIfNotFound bool) error {
	var err error
	if !FolderExists(fullPath) {
		err = errors.Errorf("directory '%s' is not found", fullPath)
		if createIfNotFound {
			err = makeDir(fullPath)
			if err != nil {
				return errors.Wrapf(err, "failed to create directory at path %s", fullPath)
			}
		}

	}

	return err
}

// checkDirectoryPaths checks for the existence of directories at the given paths or creates those if required
// It returns error for the paths that do not exist and do not need to be created
func (s *Scout) checkDirectoryPaths(fullPaths []string, createIfNotFound bool) map[string]error {
	result := map[string]error{}
	for _, fullPath := range fullPaths {
		result[fullPath] = s.checkDirectoryPath(fullPath, createIfNotFound)
	}

	return result
}
