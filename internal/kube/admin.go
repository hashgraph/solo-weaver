package kube

import (
	"os"
	"path"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/pkg/fsx"
	"golang.hedera.com/solo-weaver/pkg/security/principal"
)

const (
	// kubeConfigSourcePath is the default path to the kubernetes admin configuration file
	kubeConfigSourcePath = "/etc/kubernetes/admin.conf"
)

// KubeConfigManager manages kubeconfig file operations with injected dependencies.
type KubeConfigManager struct {
	fsManager        fsx.Manager
	principalManager principal.Manager
	kubeDir          string
}

// NewKubeConfigManager creates a new KubeConfigManager with the default dependencies.
func NewKubeConfigManager() (*KubeConfigManager, error) {
	fm, err := fsx.NewManager()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create file manager")
	}

	pm, err := principal.NewManager()
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to create principal manager")
	}

	return &KubeConfigManager{
		fsManager:        fm,
		principalManager: pm,
	}, nil
}

// SetKubeDir sets a custom kubeconfig directory path.
// If not set, the default ~/.kube will be used.
func (m *KubeConfigManager) SetKubeDir(dir string) {
	m.kubeDir = dir
}

// Configure copies the kubeconfig file to the user's home directory, to /root/.kube,
// and to the current user's directory. This allows kubectl to be used without
// requiring root privileges and ensures the config is available for all relevant users.
func (m *KubeConfigManager) Configure() error {
	// Install kubeconfig for the weaver user
	if err := m.configureWeaverKubeConfig(); err != nil {
		return err
	}

	// Install kubeconfig for the root user
	if err := m.configureRootKubeConfig(); err != nil {
		return err
	}

	// Install kubeconfig for the current user (if running with sudo)
	if err := m.configureCurrentUserKubeConfig(); err != nil {
		return err
	}

	return nil
}

// configureWeaverKubeConfig installs kubeconfig in the weaver user's home directory
// with proper ownership settings.
func (m *KubeConfigManager) configureWeaverKubeConfig() error {
	// Get the weaver service account
	svcAcc := core.ServiceAccount()

	// Lookup weaver user and group
	usr, err := m.principalManager.LookupUserByName(svcAcc.UserName)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to lookup weaver user: %s", svcAcc.UserName)
	}

	grp, err := m.principalManager.LookupGroupByName(svcAcc.GroupName)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to lookup weaver group: %s", svcAcc.GroupName)
	}

	// Determine kubeconfig directory
	kubeDir := m.kubeDir
	if kubeDir == "" {
		kubeDir = path.Join(usr.HomeDir(), ".kube")
	}

	// Create .kube directory
	err = m.fsManager.CreateDirectory(kubeDir, false)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create %s directory", kubeDir)
	}

	// Copy kubeconfig file
	kubeConfigDest := path.Join(kubeDir, "config")
	err = m.fsManager.CopyFile(kubeConfigSourcePath, kubeConfigDest, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to copy kubeconfig file %q to %q", kubeConfigSourcePath, kubeConfigDest)
	}

	// Set proper ownership
	err = m.fsManager.WriteOwner(kubeDir, usr, grp, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to set ownership of .kube directory: %q", kubeDir)
	}

	return nil
}

// configureRootKubeConfig installs kubeconfig in the root user's home directory.
func (m *KubeConfigManager) configureRootKubeConfig() error {
	rootKubeDir := "/root/.kube"

	// Create root .kube directory
	err := m.fsManager.CreateDirectory(rootKubeDir, false)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create %s directory", rootKubeDir)
	}

	// Copy kubeconfig file to root directory
	rootKubeConfigDest := path.Join(rootKubeDir, "config")
	err = m.fsManager.CopyFile(kubeConfigSourcePath, rootKubeConfigDest, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to copy kubeconfig file %q to %q", kubeConfigSourcePath, rootKubeConfigDest)
	}

	// Set proper ownership to root user and group for consistency
	rootUser, err := m.principalManager.LookupUserByName("root")
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to lookup root user")
	}

	rootGroup, err := m.principalManager.LookupGroupByName("root")
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to lookup root group")
	}

	err = m.fsManager.WriteOwner(rootKubeDir, rootUser, rootGroup, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to set ownership of root .kube directory: %q", rootKubeDir)
	}

	return nil
}

// configureCurrentUserKubeConfig installs kubeconfig in the current user's home directory.
// This is particularly useful when the application is run with sudo, as it copies the config
// to the original user's home directory (obtained from SUDO_USER environment variable).
func (m *KubeConfigManager) configureCurrentUserKubeConfig() error {
	// Get the current user from SUDO_USER environment variable
	// This contains the username of the user who invoked sudo
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		// If SUDO_USER is not set, the command wasn't run with sudo
		// In this case, we skip this configuration step
		return nil
	}

	// Don't configure if SUDO_USER is root (already handled by configureRootKubeConfig)
	if sudoUser == "root" {
		return nil
	}

	// Lookup the sudo user
	currentUser, err := m.principalManager.LookupUserByName(sudoUser)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to lookup current user: %s", sudoUser)
	}

	// Get the user's primary group
	currentGroup := currentUser.PrimaryGroup()
	if currentGroup == nil {
		return errorx.IllegalState.New("current user %s has no primary group", sudoUser)
	}

	// Determine kubeconfig directory
	currentUserKubeDir := path.Join(currentUser.HomeDir(), ".kube")

	// Create .kube directory
	err = m.fsManager.CreateDirectory(currentUserKubeDir, false)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create %s directory", currentUserKubeDir)
	}

	// Copy kubeconfig file
	currentUserKubeConfigDest := path.Join(currentUserKubeDir, "config")
	err = m.fsManager.CopyFile(kubeConfigSourcePath, currentUserKubeConfigDest, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to copy kubeconfig file %q to %q", kubeConfigSourcePath, currentUserKubeConfigDest)
	}

	// Set proper ownership to the current user
	err = m.fsManager.WriteOwner(currentUserKubeDir, currentUser, currentGroup, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to set ownership of current user .kube directory: %q", currentUserKubeDir)
	}

	return nil
}
