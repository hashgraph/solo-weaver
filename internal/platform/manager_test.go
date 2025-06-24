package platform

import (
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"os"
	"path/filepath"
	"testing"
)

const (
	testDir = "../../../../test"
)

func TestNewManager(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	m, err := NewManager()
	req.Nil(err)
	req.NotNil(m)
}

func TestGetInstallRootPath(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	m, err := NewManager()
	req.Nil(err)
	req.NotNil(m)

	installRootPath, err := m.GetInstallRootPath()
	req.NotEqual(DefaultInstallRootPath, installRootPath)
	req.NotEmpty(installRootPath)
	req.NoError(err)
}

func installFilesCleanup(t *testing.T, fsm fsx.Manager, installPath string) {
	t.Helper()
	req := require.New(t)

	dataPath := filepath.Join(installPath, dataDir)
	if fsm.IsDirectory(dataPath) {
		err := fsm.WritePermissions(filepath.Join(installPath, dataDir), 0777, true)
		req.NoError(err)
		err = os.RemoveAll(filepath.Join(installPath, dataDir))
		req.NoError(err)
	}
	os.Remove(filepath.Join(installPath, "VERSION"))
	os.Remove(filepath.Join(installPath, "config.txt"))
	os.Remove(filepath.Join(installPath, "settings.txt"))
	os.Remove(filepath.Join(installPath, "log4j2.xml"))

}
func TestInstallFilesFromPackage(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	m, err := NewManager()
	req.Nil(err)
	req.NotNil(m)

	installPath, err := m.GetInstallRootPath()
	req.NoError(err)
	fsm, err := fsx.NewManager()
	req.NoError(err)
	installFilesCleanup(t, fsm, installPath)
	err = fsm.CreateDirectory(filepath.Join(installPath, dataDir), true)
	req.NoError(err)

	extractedSDKPath := filepath.Join(testDir, "extractedSDK")
	extractedSDKDataPath := filepath.Join(extractedSDKPath, dataDir)
	err = m.InstallFilesFromPackage(extractedSDKPath, extractedSDKDataPath, false)
	req.NoError(err)
	req.FileExists(filepath.Join(installPath, "VERSION"))
	req.FileExists(filepath.Join(installPath, "config.txt"))
	req.FileExists(filepath.Join(installPath, "settings.txt"))
	req.FileExists(filepath.Join(installPath, "log4j2.xml"))
	req.DirExists(filepath.Join(installPath, dataDir))
	req.DirExists(filepath.Join(installPath, dataDir, configDir))
	req.FileExists(filepath.Join(installPath, dataDir, configDir, "config1.txt"))
	req.DirExists(filepath.Join(installPath, dataDir, keysDir))
	req.FileExists(filepath.Join(installPath, dataDir, keysDir, "keys1.key"))
	installFilesCleanup(t, fsm, installPath)
}

func TestInstallFilesFromPackage_IsUpgrade(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	m, err := NewManager()
	req.Nil(err)
	req.NotNil(m)

	installPath, err := m.GetInstallRootPath()
	req.NoError(err)
	fsm, err := fsx.NewManager()
	req.NoError(err)
	installFilesCleanup(t, fsm, installPath)
	err = fsm.CreateDirectory(filepath.Join(installPath, dataDir), true)
	req.NoError(err)

	extractedSDKPath := filepath.Join(testDir, "extractedSDK-upgrade")
	extractedSDKDataPath := filepath.Join(extractedSDKPath, dataDir)
	err = m.InstallFilesFromPackage(extractedSDKPath, extractedSDKDataPath, true)
	req.NoError(err)
	req.FileExists(filepath.Join(installPath, "VERSION"))
	req.FileExists(filepath.Join(installPath, "config.txt"))
	req.FileExists(filepath.Join(installPath, "settings.txt"))
	req.FileExists(filepath.Join(installPath, "log4j2.xml"))
	installFilesCleanup(t, fsm, installPath)
}

func TestInstallFilesFromPackage_BadExtractSDKDir(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	m, err := NewManager()
	req.Nil(err)
	req.NotNil(m)

	extractedSDKPath := filepath.Join(testDir, "not-a-dir")
	extractedSDKDataPath := filepath.Join(extractedSDKPath, dataDir)
	err = m.InstallFilesFromPackage(extractedSDKPath, extractedSDKDataPath, false)
	req.ErrorContains(err, "extractedSDKPath is empty or not a directory")
}

func TestInstallFilesFromPackage_BadExtractSDKDataDir(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	m, err := NewManager()
	req.Nil(err)
	req.NotNil(m)

	installPath, err := m.GetInstallRootPath()
	req.NoError(err)
	fsm, err := fsx.NewManager()
	req.NoError(err)
	installFilesCleanup(t, fsm, installPath)

	extractedSDKPath := filepath.Join(testDir, "extractedSDK-upgrade")
	extractedSDKDataPath := filepath.Join(extractedSDKPath, dataDir)
	err = m.InstallFilesFromPackage(extractedSDKPath, extractedSDKDataPath, false)
	req.ErrorContains(err, "extractedSDKDataPath is empty or not a directory and required for an install")
}

func TestInstallFilesFromPackage_NoExtractSDKDataDir(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	m, err := NewManager()
	req.Nil(err)
	req.NotNil(m)

	extractedSDKPath := filepath.Join(testDir, "extractedSDK")
	extractedSDKDataPath := filepath.Join(extractedSDKPath, dataDir)
	err = m.InstallFilesFromPackage(extractedSDKPath, extractedSDKDataPath, false)
	req.ErrorContains(err, "install files from package failed, install data path does not exist")
}

func TestInstallFilesFromPackage_KeysDirError(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	m, err := NewManager()
	req.Nil(err)
	req.NotNil(m)

	installPath, err := m.GetInstallRootPath()
	req.NoError(err)
	fsm, err := fsx.NewManager()
	req.NoError(err)
	installFilesCleanup(t, fsm, installPath)
	err = fsm.CreateDirectory(filepath.Join(installPath, dataDir), true)
	req.NoError(err)
	err = os.WriteFile(filepath.Join(installPath, dataDir, "keys"), make([]byte, 0), 0644)
	req.NoError(err)

	extractedSDKPath := filepath.Join(testDir, "extractedSDK")
	extractedSDKDataPath := filepath.Join(extractedSDKPath, dataDir)
	err = m.InstallFilesFromPackage(extractedSDKPath, extractedSDKDataPath, false)
	req.ErrorContains(err, "install files from package failed, unable to install keys directory")
	installFilesCleanup(t, fsm, installPath)
}

func TestInstallFilesFromPackage_ConfigTxtError(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	m, err := NewManager()
	req.Nil(err)
	req.NotNil(m)

	installPath, err := m.GetInstallRootPath()
	req.NoError(err)
	fsm, err := fsx.NewManager()
	req.NoError(err)
	installFilesCleanup(t, fsm, installPath)
	err = fsm.CreateDirectory(filepath.Join(installPath, dataDir), true)
	req.NoError(err)
	err = fsm.CreateDirectory(filepath.Join(installPath, "config.txt"), true)
	req.NoError(err)

	extractedSDKPath := filepath.Join(testDir, "extractedSDK")
	extractedSDKDataPath := filepath.Join(extractedSDKPath, dataDir)
	err = m.InstallFilesFromPackage(extractedSDKPath, extractedSDKDataPath, false)
	req.ErrorContains(err, "install files from package failed, unable to install misc directory")
	installFilesCleanup(t, fsm, installPath)
}

func TestInstallFilesFromPackage_ConfigDirError(t *testing.T) {
	// Simplify repetitive require by avoiding the need to repeat the testing.T argument.
	req := require.New(t)

	fsm, err := fsx.NewManager()
	req.NoError(err)
	m, err := NewManager(WithLogger(logx.Nop()), WithFileSystemManager(fsm))
	req.Nil(err)
	req.NotNil(m)

	installPath, err := m.GetInstallRootPath()
	req.NoError(err)
	installFilesCleanup(t, fsm, installPath)
	err = fsm.CreateDirectory(filepath.Join(installPath, dataDir), true)
	req.NoError(err)
	err = os.WriteFile(filepath.Join(installPath, dataDir, "config"), make([]byte, 0), 0644)
	req.NoError(err)

	extractedSDKPath := filepath.Join(testDir, "extractedSDK")
	extractedSDKDataPath := filepath.Join(extractedSDKPath, dataDir)
	err = m.InstallFilesFromPackage(extractedSDKPath, extractedSDKDataPath, false)
	req.ErrorContains(err, "install files from package failed, unable to install config directory")
	installFilesCleanup(t, fsm, installPath)
}
