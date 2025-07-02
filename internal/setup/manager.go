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

package setup

import (
	"context"
	"embed"
	"fmt"
	extract "github.com/codeclysm/extract/v3"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/internal/platform"
	"golang.hedera.com/solo-provisioner/pkg/backup"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"io"
	fs2 "io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed images/*
var imagesFolder embed.FS

type setupManager struct {
	installWorkingDirectory string
	logger                  *zerolog.Logger
	fileSystemManager       fsx.Manager
	logPrefix               string
	dockerNodeImageName     string
	backupManager           backup.Manager
	platformManager         platform.Manager
}

const (
	sdkPackageDirName       = "sdk-package"
	dockerImageDirName      = "docker-image-staging"
	networkNodeSuffix       = "-network-node"
	sdkDirName              = "sdk"
	networkNodeBaseImage    = "network-node-base"
	networkNodeHavegedImage = "network-node-haveged"
	sdkDataDirName          = "data"
	rootImagesDirName       = "images"
)

func (sm *setupManager) CreateStagingArea() error {
	installWorkingDirectory, err := os.MkdirTemp("", "nmt_setup_workdir.*")
	if err != nil {
		return errorx.IllegalArgument.New("failed to create working directory").WithUnderlyingErrors(err)
	}

	sm.installWorkingDirectory = installWorkingDirectory
	sm.logger.Debug().
		Str(logFields.workingDirectory, sm.installWorkingDirectory).
		Msg("created working directory")

	return nil
}

func (sm *setupManager) GetInstallWorkingDirectory() string {
	return sm.installWorkingDirectory
}

func retainTempFlag() bool {
	if os.Getenv("NMT_RETAIN_TEMP") == "true" {
		return true
	}
	return false
}

func (sm *setupManager) Cleanup() error {
	if retainTempFlag() {
		sm.logger.Info().
			Str(logFields.logPrefix, sm.logPrefix).
			Str(logFields.workingDirectory, sm.installWorkingDirectory).
			Msgf("%s: Working Directory Path [ path = '%s' ]", sm.logPrefix, sm.installWorkingDirectory)
	} else {
		err := os.RemoveAll(sm.installWorkingDirectory)
		if err != nil {
			return errorx.IllegalState.New("failed to remove working directory").WithUnderlyingErrors(err)
		}
		sm.installWorkingDirectory = ""
	}

	return nil
}

func (sm *setupManager) extractedSDKPath() string {
	return filepath.Join(sm.installWorkingDirectory, sdkPackageDirName)
}

func (sm *setupManager) ExtractSDKArchive(ctx context.Context, sdkPackageFile string) error {
	if sm.GetInstallWorkingDirectory() == "" {
		return errorx.IllegalArgument.New("working directory not set, staging area must be created as a prior step")
	}

	if !sm.fileSystemManager.IsRegularFile(sdkPackageFile) {
		errorMessage := fmt.Sprintf("%s: SDK package file does not exist or is not a regular file [ path = '%s' ]", sm.logPrefix, sdkPackageFile)
		return errorx.IllegalArgument.New(errorMessage)

	}

	sm.logger.Info().
		Str(logFields.logPrefix, sm.logPrefix).
		Str(logFields.sdkPackageFile, sdkPackageFile).
		Msg("Setup: extracting SDK package")

	extractedSDKPath := sm.extractedSDKPath()

	err := sm.extractArchive(ctx, sdkPackageFile, extractedSDKPath)
	if err != nil {
		errorMessage := fmt.Sprintf("%s: failed to extract SDK package [ archive = '%s' ]", sm.logPrefix, sdkPackageFile)
		return errorx.IllegalArgument.New(errorMessage)
	}

	sm.logger.Info().
		Str(logFields.logPrefix, sm.logPrefix).
		Str(logFields.sdkPackageFile, sdkPackageFile).
		Str(logFields.extractedSDKPath, extractedSDKPath).
		Msg("Setup: extracted SDK package")

	return nil
}

func (sm *setupManager) extractArchive(ctx context.Context, sdkPackageFileName string, extractedSDKPath string) error {
	sdkPackageFileNameString := strings.ToLower(sdkPackageFileName)
	if !strings.HasSuffix(sdkPackageFileNameString, ".tar") && !strings.HasSuffix(sdkPackageFileNameString, ".tar.gz") && !strings.HasSuffix(sdkPackageFileNameString, ".zip") {
		errorMessage := fmt.Sprintf("%s: package file must be *.tar, *.tar.gz, or *.zip [ path = '%s' ]", sm.logPrefix, sdkPackageFileName)
		return errorx.IllegalArgument.New(errorMessage)
	}

	if !sm.fileSystemManager.IsDirectory(extractedSDKPath) {
		err := sm.fileSystemManager.CreateDirectory(extractedSDKPath, true)
		if err != nil {
			errorMessage := fmt.Sprintf("%s: failed to create SDK package directory [ path = '%s' ]", sm.logPrefix, extractedSDKPath)
			return errorx.IllegalState.New(errorMessage)

		}
	}

	sdkPackageFile, err := os.Open(sdkPackageFileName)
	if err != nil {
		errorMessage := fmt.Sprintf("%s: failed to read SDK package file [ path = '%s' ]", sm.logPrefix, sdkPackageFileName)
		return errorx.IllegalState.New(errorMessage)

	}

	err = extract.Archive(ctx, sdkPackageFile, extractedSDKPath, nil)
	if err != nil {
		errorMessage := fmt.Sprintf("%s: failed to extract SDK package file [ path = '%s' ]", sm.logPrefix, sdkPackageFileName)
		return errorx.IllegalState.New(errorMessage).WithUnderlyingErrors(err)

	}

	return nil
}

func (sm *setupManager) dockerImageDestinationPath() string {
	return filepath.Join(sm.installWorkingDirectory, dockerImageDirName)
}

func (sm *setupManager) PrepareDocker(imageID string) error {
	dockerImageDestinationPath := sm.dockerImageDestinationPath()

	sm.logger.Info().
		Str(logFields.logPrefix, sm.logPrefix).
		Str(logFields.dockerImageDestinationPath, dockerImageDestinationPath).
		Msg("Setup: preparing Docker Image Build Environment")

	err := sm.prepareDockerImageBuild(dockerImageDestinationPath, imageID)
	if err != nil {
		errorMessage := fmt.Sprintf("%s: failed to prepare Docker Image Build Environment [ path = '%s' ]", sm.logPrefix, dockerImageDestinationPath)
		return errorx.IllegalArgument.New(errorMessage)
	}

	sm.logger.Info().
		Str(logFields.logPrefix, sm.logPrefix).
		Str(logFields.dockerImageDestinationPath, dockerImageDestinationPath).
		Msg("Setup: prepared Docker Image Build Environment")

	return nil
}

func (sm *setupManager) prepareDockerImageBuild(dockerImageDestinationPath string, imageID string) error {
	if !sm.fileSystemManager.IsDirectory(sm.installWorkingDirectory) {
		errorMessage := fmt.Sprintf("%s: prepare staging area has not been called, exiting", sm.logPrefix)
		return errorx.IllegalArgument.New(errorMessage)
	}

	if dockerImageDestinationPath == "" {
		errorMessage := fmt.Sprintf("%s: dockerImageDestinationPath is empty", sm.logPrefix)
		return errorx.IllegalArgument.New(errorMessage)
	}

	if sm.fileSystemManager.IsDirectory(dockerImageDestinationPath) {
		err := os.RemoveAll(dockerImageDestinationPath)
		if err != nil {
			errorMessage := fmt.Sprintf("%s: failed to cleanup of existing Docker Image Build Environment [ path = '%s' ]", sm.logPrefix, dockerImageDestinationPath)
			return errorx.IllegalState.New(errorMessage).WithUnderlyingErrors(err)
		}
	}

	err := sm.fileSystemManager.CreateDirectory(dockerImageDestinationPath, true)
	if err != nil {
		errorMessage := fmt.Sprintf("%s: failed to create Docker Image Build Environment [ path = '%s' ]", sm.logPrefix, dockerImageDestinationPath)
		return errorx.IllegalState.New(errorMessage).WithUnderlyingErrors(err)

	}

	dockerImages := []string{
		networkNodeBaseImage,
		networkNodeHavegedImage,
		imageID + networkNodeSuffix,
	}

	for _, img := range dockerImages {
		sourceFolder := filepath.Join("images/", img)
		imageWorkingFolder := filepath.Join(dockerImageDestinationPath, img)
		sm.logger.Debug().
			Str(logFields.logPrefix, sm.logPrefix).
			Str(logFields.imageName, img).
			Str(logFields.sourceFolder, sourceFolder).
			Str(logFields.imageWorkingFolder, imageWorkingFolder).
			Msg("Setup: preparing Docker Image Build Environment")

		err = sm.copyEmbedDirectory(sourceFolder, dockerImageDestinationPath)
		if err != nil {
			errorMessage := fmt.Sprintf("%s: failed to copy Docker Image Build Environment [ imageName = '%s', sourceFolder = '%s' ]", sm.logPrefix, img, sourceFolder)
			return errorx.IllegalState.New(errorMessage).WithUnderlyingErrors(err)

		}

		imageSDKWorkingFolder := filepath.Join(imageWorkingFolder, sdkDirName)
		if strings.HasSuffix(img, networkNodeSuffix) && !sm.fileSystemManager.IsDirectory(imageSDKWorkingFolder) {
			sm.dockerNodeImageName = img
			sm.logger.Debug().
				Str(logFields.logPrefix, sm.logPrefix).
				Str(logFields.imageName, img).
				Msg("Setup: preparing Docker Image Build Environment: creating missing SDK working folder")

			err := sm.fileSystemManager.CreateDirectory(imageSDKWorkingFolder, true)
			if err != nil {
				errorMessage := fmt.Sprintf("%s: failed to create Docker Image Build Environment: creating missing SDK working folder [ imageName = '%s', errorCode = '%s' ]", sm.logPrefix, img, err.Error())
				return errorx.IllegalState.New(errorMessage).WithUnderlyingErrors(err)

			}
		}
	}

	return nil
}

func (sm *setupManager) copyEmbedDirectory(sourceFolder string, dockerImageDestinationPath string) error {
	return fs2.WalkDir(imagesFolder, sourceFolder, func(path string, d fs2.DirEntry, err error) error {
		return sm.handleWalkDir(imagesFolder, path, d, err, dockerImageDestinationPath)
	})
}

func (sm *setupManager) handleWalkDir(imagesFolder embed.FS, path string, d fs2.DirEntry, err error, dockerImageDestinationPath string) error {
	if err != nil {
		return err
	}

	fi, err := d.Info()
	if err != nil {
		return err
	}

	// Get the relative path of the file rooted at the source directory
	relPath, err := filepath.Rel(rootImagesDirName, path)
	if err != nil {
		return fsx.NewFileSystemError(err, "unable to determine relative path", path)
	}

	if sm.fileSystemManager.IsDirectoryByFileInfo(fi) {
		return sm.fileSystemManager.CreateDirectory(filepath.Join(dockerImageDestinationPath, relPath), true)
	} else if sm.fileSystemManager.IsRegularFileByFileInfo(fi) {
		sourceFile, err := imagesFolder.Open(path)
		if err != nil {
			return fsx.NewFileSystemError(err, "failed to open the source file", path)
		}

		destFile, err := os.Create(filepath.Join(dockerImageDestinationPath, relPath))
		if err != nil {
			destPath := filepath.Join(dockerImageDestinationPath, relPath)
			return fsx.NewFileSystemError(err, "failed to create the destination file", destPath)
		}
		defer destFile.Close()

		_, err = io.Copy(destFile, sourceFile)
		if err != nil {
			return fsx.NewFileSystemError(err, "failed to copy the file contents", path)
		}
		err = destFile.Sync()
		if err != nil {
			return fsx.NewFileSystemError(err, "failed to sync the destination file", destFile.Name())
		}
	} else {
		return fsx.NewFileSystemError(nil, "unsupported file type", path)
	}
	return nil
}

func (sm *setupManager) StageSDK(sdkPackageFile string) error {
	if !sm.fileSystemManager.IsDirectory(sm.installWorkingDirectory) {
		errorMessage := fmt.Sprintf("%s: prepare staging area has not been called, exiting", sm.logPrefix)
		return errorx.IllegalArgument.New(errorMessage)
	}

	if !sm.fileSystemManager.IsRegularFile(sdkPackageFile) {
		errorMessage := fmt.Sprintf("%s: SDK package file either does not exist or is not a regular file [ sdkPackageFile = '%s' ]", sm.logPrefix, sdkPackageFile)
		return errorx.IllegalArgument.New(errorMessage)
	}

	dockerImageDestinationPath := sm.dockerImageDestinationPath()
	if !sm.fileSystemManager.IsDirectory(dockerImageDestinationPath) {
		errorMessage := fmt.Sprintf("%s: docker image destination path doesn't exist, PrepareDocker() is required to be called as a predecessor [ dockerImageDestinationPath = '%s' ] ", sm.logPrefix, dockerImageDestinationPath)
		return errorx.IllegalArgument.New(errorMessage)
	}

	extractedSDKPath := sm.extractedSDKPath()
	if !sm.fileSystemManager.IsDirectory(extractedSDKPath) {
		errorMessage := fmt.Sprintf("%s: extracted sdk destination path doesn't exist, ExtractSDKArchive() is required to be called as a predecessor [ extractedSDKPath = '%s' ] ", sm.logPrefix, extractedSDKPath)
		return errorx.IllegalArgument.New(errorMessage)
	}

	sm.logger.Info().
		Str(logFields.logPrefix, sm.logPrefix).
		Str(logFields.sdkPackageFile, sdkPackageFile).
		Str(logFields.extractedSDKPath, extractedSDKPath).
		Str(logFields.dockerImageDestinationPath, dockerImageDestinationPath).
		Msg("Setup: staging SDK")

	err := sm.prepareSDKFiles(sdkPackageFile, extractedSDKPath, dockerImageDestinationPath)
	if err != nil {
		sm.logger.Error().
			Str(logFields.logPrefix, sm.logPrefix).
			Str(logFields.sdkPackageFile, sdkPackageFile).
			Str(logFields.extractedSDKPath, extractedSDKPath).
			Str(logFields.dockerImageDestinationPath, dockerImageDestinationPath).
			Str(logFields.errorCode, err.Error()).
			Msg("Setup: failed to stage SDK")
		return err
	}

	sm.logger.Info().
		Str(logFields.logPrefix, sm.logPrefix).
		Str(logFields.sdkPackageFile, sdkPackageFile).
		Str(logFields.extractedSDKPath, extractedSDKPath).
		Str(logFields.dockerImageDestinationPath, dockerImageDestinationPath).
		Msg("Setup: staged SDK")

	return nil
}

func (sm *setupManager) prepareSDKFiles(sdkPackageFile string, extractedSDKPath string, dockerImageDestinationPath string) error {
	networkNodePath := filepath.Join(dockerImageDestinationPath, sm.GetDockerNodeImageName())
	if !sm.fileSystemManager.IsDirectory(networkNodePath) {
		errorMessage := fmt.Sprintf("%s: network node path doesn't exist [ networkNodePath = '%s' ]", sm.logPrefix, networkNodePath)
		return errorx.IllegalArgument.New(errorMessage)
	}

	sm.logger.Debug().
		Str(logFields.logPrefix, sm.logPrefix).
		Str(logFields.sdkPackageFile, sdkPackageFile).
		Str(logFields.extractedSDKPath, extractedSDKPath).
		Str(logFields.dockerImageDestinationPath, dockerImageDestinationPath).
		Str(logFields.networkNodePath, networkNodePath).
		Msg("Setup: prepare SDK files : found node image recipe")

	networkNodeSDKDataPath := filepath.Join(networkNodePath, sdkDirName, sdkDataDirName)

	if !sm.fileSystemManager.IsDirectory(networkNodeSDKDataPath) {
		err := sm.fileSystemManager.CreateDirectory(networkNodeSDKDataPath, true)
		if err != nil {
			errorMessage := fmt.Sprintf("%s: failed to create directory [ networkNodeSDKDataPath = '%s', errorCode = '%s' ]", sm.logPrefix, networkNodeSDKDataPath, err.Error())
			return errorx.IllegalState.New(errorMessage).WithUnderlyingErrors(err)
		}
	}

	extractedSDKDataPath, err := sm.extractedSDKDataPath(extractedSDKPath)
	if err != nil {
		errorMessage := fmt.Sprintf("%s: failed to get extracted sdk data path [ extractedSDKPath = '%s', errorCode = '%s' ]", sm.logPrefix, extractedSDKPath, err.Error())
		return errorx.IllegalState.New(errorMessage).WithUnderlyingErrors(err)
	}

	err = sm.copySDKDataFiles(extractedSDKDataPath, networkNodeSDKDataPath)
	if err != nil {
		errorMessage := fmt.Sprintf("%s: failed to copy sdk data files [ extractedSDKDataPath = '%s', networkNodeSDKDataPath = '%s', errorCode = '%s' ]", sm.logPrefix, extractedSDKDataPath, networkNodeSDKDataPath, err.Error())
		return errorx.IllegalState.New(errorMessage).WithUnderlyingErrors(err)
	}

	err = sm.platformManager.InstallFilesFromPackage(extractedSDKPath, extractedSDKDataPath, false)
	if err != nil {
		errorMessage := fmt.Sprintf("%s: failed to install sdk files from package [ extractedSDKPath = '%s', extractedSDKDataPath = '%s', errorCode = '%s' ]", sm.logPrefix, extractedSDKPath, extractedSDKDataPath, err.Error())
		return errorx.IllegalState.New(errorMessage).WithUnderlyingErrors(err)
	}

	return nil
}

func (sm *setupManager) GetDockerNodeImageName() string {
	return sm.dockerNodeImageName
}

func (sm *setupManager) extractedSDKDataPath(extractedSDKPath string) (string, error) {
	extractedSDKDataLocations := []string{
		"data",
		"sdk/data",
		"hedera-node/data",
	}

	for _, extractedSDKDataLocation := range extractedSDKDataLocations {
		extractedSDKDataPath := filepath.Join(extractedSDKPath, extractedSDKDataLocation)
		if sm.fileSystemManager.IsDirectory(extractedSDKDataPath) {
			sm.logger.Debug().
				Str(logFields.logPrefix, sm.logPrefix).
				Str(logFields.extractedSDKDataPath, extractedSDKDataPath).
				Msg("Setup: extracted SDK data path found")
			return extractedSDKDataPath, nil
		}
	}

	errorMessage := fmt.Sprintf("%s: unable to locate extracted SDK data path [ extractedSDKPath = '%s' ]", sm.logPrefix, extractedSDKPath)
	return "", errorx.IllegalArgument.New(errorMessage)
}

func (sm *setupManager) copySDKDataFiles(extractedSDKDataPath string, networkNodeSDKDataPath string) error {
	requiredDataFolders := []string{
		"apps",
		"lib",
	}

	for _, requiredDataFolder := range requiredDataFolders {
		requiredDataFolderPath := filepath.Join(extractedSDKDataPath, requiredDataFolder)

		if !sm.fileSystemManager.IsDirectory(requiredDataFolderPath) {
			errorMessage := fmt.Sprintf("%s: required data folder not found [ requiredDataFolderPath = '%s' ]", sm.logPrefix, requiredDataFolderPath)
			return errorx.IllegalState.New(errorMessage)

		}

		err := sm.backupManager.CopyTree(requiredDataFolderPath, networkNodeSDKDataPath)
		if err != nil {
			errorMessage := fmt.Sprintf("%s: failed to copy data folder [ requiredDataFolderPath = '%s', networkNodeSDKDataPath = '%s', errorCode = '%s' ]", sm.logPrefix, requiredDataFolderPath, networkNodeSDKDataPath, err.Error())
			return errorx.IllegalState.New(errorMessage).WithUnderlyingErrors(err)
		}
	}

	return nil
}

// Option allows injecting various parameters for SetupManager
type Option = func(s *setupManager)

// WithLogger allows injecting a logger for the Manager
func WithLogger(logger *zerolog.Logger) Option {
	return func(s *setupManager) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// WithFileSystemManager allows injecting a fs.Manager for the Manager
func WithFileSystemManager(fileSystemManager fsx.Manager) Option {
	return func(sm *setupManager) {
		if fileSystemManager != nil {
			sm.fileSystemManager = fileSystemManager
		}
	}
}

// WithBackupManager allows injecting a backup.Manager for the Manager
func WithBackupManager(backupManager backup.Manager) Option {
	return func(sm *setupManager) {
		if backupManager != nil {
			sm.backupManager = backupManager
		}
	}
}

// WithPlatformManager allows injecting a platform.Manager for the Manager
func WithPlatformManager(platformManager platform.Manager) Option {
	return func(sm *setupManager) {
		if platformManager != nil {
			sm.platformManager = platformManager
		}
	}
}

func NewManager(logPrefix string, opts ...Option) (Manager, error) {
	if logPrefix != "" {
		logPrefix = "install"
	}
	sm := &setupManager{
		logger:    logx.Nop(),
		logPrefix: logPrefix,
	}

	for _, opt := range opts {
		opt(sm)
	}

	// set the default file system manager if one was not provided
	if sm.fileSystemManager == nil {
		fileSystemManager, err := fsx.NewManager()
		if err != nil {
			return nil, err
		}

		sm.fileSystemManager = fileSystemManager
	}

	// set the default backup manager if one is not provided
	if sm.backupManager == nil {
		backupManager, err := backup.NewManager()
		if err != nil {
			return nil, err
		}

		sm.backupManager = backupManager
	}

	// set the default platform manager if one is not provided
	if sm.platformManager == nil {
		platformManager, err := platform.NewManager()
		if err != nil {
			return nil, err
		}

		sm.platformManager = platformManager
	}

	return sm, nil
}
