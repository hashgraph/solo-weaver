// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"os"
	"path"

	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/joomcode/errorx"
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

// Configure copies the kubeconfig file to /root/.kube, the current sudo user's home,
// and the weaver service account's home. This allows kubectl to be used without
// requiring root privileges and ensures the config is available for all relevant users.
func (m *KubeConfigManager) Configure() error {
	if err := m.configureRootKubeConfig(); err != nil {
		return err
	}
	if err := m.configureCurrentUserKubeConfig(); err != nil {
		return err
	}
	if err := m.configureWeaverKubeConfig(); err != nil {
		return err
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

// configureWeaverKubeConfig installs kubeconfig in the weaver service account's home directory.
// Uses config.WeaverHomeDir() directly rather than user.HomeDir() so it works correctly on
// existing hosts where /etc/passwd still records home=/ from an older install.
// Skips gracefully if the weaver user has not been provisioned yet.
func (m *KubeConfigManager) configureWeaverKubeConfig() error {
	weaverUser, err := m.principalManager.LookupUserByName(config.WeaverUserName())
	if err != nil {
		if !errorx.IsOfType(err, principal.UserNotFoundError) {
			return errorx.IllegalState.Wrap(err, "failed to lookup weaver user")
		}
		return nil
	}

	weaverGroup, err := m.principalManager.LookupGroupByName(config.WeaverGroupName())
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to lookup weaver group")
	}

	kubeDir := m.kubeDir
	if kubeDir == "" {
		kubeDir = path.Join(config.WeaverHomeDir(), ".kube")
	}

	if err := m.fsManager.CreateDirectory(kubeDir, false); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create %s directory", kubeDir)
	}

	destPath := path.Join(kubeDir, "config")
	if err := m.fsManager.CopyFile(kubeConfigSourcePath, destPath, true); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to copy kubeconfig to %s", destPath)
	}

	if err := m.fsManager.WriteOwner(kubeDir, weaverUser, weaverGroup, true); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to set ownership of %s", kubeDir)
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

	// If SUDO_USER is not set, the command wasn't run with sudo
	// In this case, we skip this configuration step
	// Also, don't configure if SUDO_USER is root (already handled by configureRootKubeConfig)
	if sudoUser == "" || sudoUser == "root" {
		return nil
	}

	// Reject SUDO_USER values that would be unsafe to pass downstream. Env vars
	// can be manipulated by attackers so this gate must run before any use of
	// sudoUser in NSS lookups or path construction.
	if err := sanity.ValidateUsername(sudoUser); err != nil {
		return errorx.IllegalState.Wrap(err, "invalid SUDO_USER environment variable: %s", sudoUser)
	}

	currentUser, err := m.principalManager.LookupUserByName(sudoUser)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to lookup current user: %s", sudoUser)
	}

	currentGroup := currentUser.PrimaryGroup()
	if currentGroup == nil {
		return errorx.IllegalState.New("current user %s has no primary group", sudoUser)
	}

	// Determine kubeconfig directory using the validated user's home directory
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
