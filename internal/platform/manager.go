package platform

import (
	"fmt"
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/pkg/backup"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"os"
	"path/filepath"
	"strings"
)

const (
	logPrefix          = "Platform Install"
	dataDir            = "data"
	keysDir            = "keys"
	configDir          = "config"
	sdkDir             = "sdk"
	hederaDir          = "hedera-node"
	configFileName     = "config.txt"
	settingsFileName   = "settings.txt"
	log4j2FileName     = "log4j2.xml"
	hederaCertFileName = "hedera.cert"
	hederaKeyFileName  = "hedera.key"
	versionFileName    = "VERSION"
)

type platformManager struct {
	logger            *zerolog.Logger
	fileSystemManager fsx.Manager
}

func (p *platformManager) InstallFilesFromPackage(extractedSDKPath string, extractedSDKDataPath string, isUpgrade bool) error {
	if extractedSDKPath == "" || !p.fileSystemManager.IsDirectory(extractedSDKPath) {
		errorMessage := fmt.Sprintf("%s: install files from package failed, extractedSDKPath is empty or not a directory [ extractedSDKPath = %s ]", logPrefix, extractedSDKPath)
		return errors.New(errorMessage)
	}

	if !isUpgrade && (extractedSDKDataPath == "" || !p.fileSystemManager.IsDirectory(extractedSDKDataPath)) {
		errorMessage := fmt.Sprintf("%s: install files from package failed, extractedSDKDataPath is empty or not a directory and required for an install [ extractedSDKDataPath = %s ]", logPrefix, extractedSDKDataPath)
		return errors.New(errorMessage)
	}

	installPath, err := p.GetInstallRootPath()
	if err != nil {
		errorMessage := fmt.Sprintf("%s: install files from package failed, failed to get install path", logPrefix)
		return errors.New(errorMessage)
	}

	if !p.fileSystemManager.IsDirectory(installPath) {
		errorMessage := fmt.Sprintf("%s: install files from package failed, install path does not exist [ installPath = %s ]", logPrefix, installPath)
		return errors.New(errorMessage)
	}

	installDataPath := filepath.Join(installPath, dataDir)
	if !p.fileSystemManager.IsDirectory(installDataPath) {
		errorMessage := fmt.Sprintf("%s: install files from package failed, install data path does not exist [ installDataPath = %s ]", logPrefix, installDataPath)
		return errors.New(errorMessage)
	}

	p.logger.Info().
		Str(logFields.extractedSDKPath, extractedSDKPath).
		Str(logFields.extractedSDKDataPath, extractedSDKDataPath).
		Msg("Platform: preparing to install platform specific files")

	extractedSDKKeysPath := filepath.Join(extractedSDKDataPath, keysDir)
	if p.fileSystemManager.IsDirectory(extractedSDKKeysPath) {
		err := p.installFolderContentsFromPackage(extractedSDKKeysPath, installDataPath, keysDir)
		if err != nil {
			errorMessage := fmt.Sprintf("%s: install files from package failed, "+
				"unable to install keys directory [ extractedSDKKeysPath = %s, installDataPath = %s, errorCode = %s ]",
				logPrefix, extractedSDKKeysPath, installDataPath, err.Error())
			return errors.Wrap(err, errorMessage)
		}
	} else {
		p.logger.Info().
			Str(logFields.extractedSDKKeysPath, extractedSDKKeysPath).
			Msg("Platform: install files from package, keys directory not found in package, skipping install")
	}

	extractedSDKConfigPath := filepath.Join(extractedSDKDataPath, configDir)
	if p.fileSystemManager.IsDirectory(extractedSDKConfigPath) {
		err := p.installFolderContentsFromPackage(extractedSDKConfigPath, installDataPath, configDir)
		if err != nil {
			errorMessage := fmt.Sprintf("%s: install files from package failed, unable to install config directory [ extractedSDKConfigPath = %s, installDataPath = %s, errorCode = %s ]", logPrefix, extractedSDKConfigPath, installDataPath, err.Error())
			return errors.Wrap(err, errorMessage)
		}
	} else {
		p.logger.Info().
			Str(logFields.extractedSDKConfigPath, extractedSDKConfigPath).
			Msg("Platform: install files from package, config directory not found in package, skipping install")
	}

	err = p.installMiscFromPackage(extractedSDKPath, extractedSDKDataPath, installPath, installDataPath, isUpgrade)
	if err != nil {
		errorMessage := fmt.Sprintf("%s: install files from package failed, unable to install misc directory [ extractedSDKPath = %s, extractedSDKDataPath = %s, installPath = %s, installDataPath = %s, errorCode = %s ]", logPrefix, extractedSDKPath, extractedSDKDataPath, installPath, installDataPath, err.Error())
		return errors.Wrap(err, errorMessage)
	}

	return nil
}

func (p *platformManager) installMiscFromPackage(extractedSDKPath string, extractedSDKDataPath string, installPath string, installDataPath string, isUpgrade bool) error {
	if !isUpgrade {
		if !p.fileSystemManager.IsDirectory(installDataPath) {
			errorMessage := fmt.Sprintf("%s: install misc from package failed, install data path %s does not exist", logPrefix, installDataPath)
			return errors.New(errorMessage)
		}
		if !p.fileSystemManager.IsDirectory(extractedSDKDataPath) {
			errorMessage := fmt.Sprintf("%s: install misc from package failed, extracted sdk data path %s does not exist", logPrefix, extractedSDKDataPath)
			return errors.New(errorMessage)
		}
	}

	searchPath := []string{
		extractedSDKPath,
		filepath.Join(extractedSDKPath, sdkDir),
		filepath.Join(extractedSDKPath, hederaDir),
	}

	err := p.installFile(searchPath, configFileName, installPath)
	if err != nil {
		return err
	}

	err = p.installFile(searchPath, settingsFileName, installPath)
	if err != nil {
		return err
	}

	err = p.installFile(searchPath, log4j2FileName, installPath)
	if err != nil {
		return err
	}

	err = p.installFile(searchPath, versionFileName, installPath)
	if err != nil {
		return err
	}

	hederaCertFilePath := p.locateFile(searchPath, hederaCertFileName)
	if hederaCertFilePath != "" {
		p.logger.Warn().
			Str(logFields.extractedSDKDataPath, extractedSDKDataPath).
			Str(logFields.hederaCertFilePath, hederaCertFilePath).
			Msgf("Platform: discovered SECURITY RISK, %s found in SDK package file", hederaCertFileName)
	}

	hederaKeyFilePath := p.locateFile(searchPath, hederaKeyFileName)
	if hederaKeyFilePath != "" {
		p.logger.Warn().
			Str(logFields.extractedSDKDataPath, extractedSDKDataPath).
			Str(logFields.hederaCertFilePath, hederaCertFilePath).
			Msgf("Platform: discovered SECURITY RISK, %s found in SDK package file", hederaKeyFilePath)
	}

	return nil
}

func (p *platformManager) installFile(searchPath []string, fileName string, installPath string) error {
	filePath := p.locateFile(searchPath, fileName)
	if filePath != "" {
		err := p.fileSystemManager.CopyFile(filePath, installPath, true)
		if err != nil {
			return err
		}

		err = p.fileSystemManager.WritePermissions(filepath.Join(installPath, fileName), fsx.DefaultROPermissions, false)
		if err != nil {
			return err
		}
		p.logger.Info().
			Str(logFields.fileName, fileName).
			Str(logFields.installPath, installPath).
			Msg("Platform: install misc from SDK package succeeded, file copied")
	} else {
		p.logger.Warn().
			Str(logFields.fileName, fileName).
			Msg("Platform: install misc from SDK package failed, file not found in SDK package, skipping install")
	}

	return nil
}

func (p *platformManager) locateFile(locations []string, fileName string) string {
	for _, location := range locations {
		filePath := filepath.Join(location, fileName)
		if p.fileSystemManager.IsRegularFile(filePath) {
			return filePath
		}
	}
	return ""
}

func (p *platformManager) installFolderContentsFromPackage(sourceFolderPath string, destinationFolderPath string, destinationFolderName string) error {
	destinationPath := filepath.Join(destinationFolderPath, destinationFolderName)
	if !p.fileSystemManager.IsDirectory(destinationPath) {
		err := p.fileSystemManager.CreateDirectory(destinationPath, true)
		if err != nil {
			return err
		}
	}

	backupManager, err := backup.NewManager(backup.WithFileSystemManager(p.fileSystemManager))
	if err != nil {
		return err
	}

	filter, err := backup.NewDirAndPFXFilter(p.fileSystemManager, sourceFolderPath, p.logger)
	if err != nil {
		return err
	}

	err = backupManager.CopyTreeByFilter(sourceFolderPath, destinationPath, filter)
	if err != nil {
		return err
	}

	err = p.fileSystemManager.WritePermissions(destinationPath, fsx.DefaultROPermissions, true)
	if err != nil {
		return err
	}

	return nil
}

func (p *platformManager) GetInstallRootPath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	executablePath := filepath.Dir(executable)
	nmtToolsPath := filepath.Dir(executablePath)
	nmtRootPath := filepath.Dir(nmtToolsPath)

	if strings.HasSuffix(filepath.Dir(nmtRootPath), "tmp") {
		return DefaultInstallRootPath, nil
	}

	return nmtRootPath, nil
}

// Option allows injecting various parameters for SetupManager
type Option = func(pm *platformManager)

// WithLogger allows injecting a logger for the Manager
func WithLogger(logger *zerolog.Logger) Option {
	return func(pm *platformManager) {
		if logger != nil {
			pm.logger = logger
		}
	}
}

// WithFileSystemManager allows injecting a fs.Manager for the Manager
func WithFileSystemManager(fileSystemManager fsx.Manager) Option {
	return func(pm *platformManager) {
		if fileSystemManager != nil {
			pm.fileSystemManager = fileSystemManager
		}
	}
}

func NewManager(opts ...Option) (Manager, error) {
	pm := &platformManager{
		logger: logx.Nop(),
	}

	for _, opt := range opts {
		opt(pm)
	}

	// set the default file system manager if one was not provided
	if pm.fileSystemManager == nil {
		fileSystemManager, err := fsx.NewManager()
		if err != nil {
			return nil, err
		}

		pm.fileSystemManager = fileSystemManager
	}

	return pm, nil
}
