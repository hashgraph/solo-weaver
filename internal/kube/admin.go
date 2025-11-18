package kube

import (
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

// Configure copies the kubeconfig file to the user's home directory and to /root/.kube,
// and sets the ownership to the current user. This allows kubectl to be used without
// requiring root privileges and ensures the config is available for the root user.
func (m *KubeConfigManager) Configure() error {
	// Install kubeconfig for the weaver user
	if err := m.configureWeaverKubeConfig(); err != nil {
		return err
	}

	// Install kubeconfig for the root user
	if err := m.configureRootKubeConfig(); err != nil {
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
