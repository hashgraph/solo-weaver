package software

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path"
	"strings"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/network"
	"golang.hedera.com/solo-weaver/internal/templates"
)

var (
	rootPath                  = string(os.PathSeparator)
	kubeletServiceDirRelPath  = path.Join("usr", "lib", "systemd", "system", "kubelet.service.d")
	kubeadmConfFileName       = "10-kubeadm.conf"
	kubeadmInitConfigFileName = "kubeadm-init.yaml"
	etcWeaverDirRelPath       = path.Join("etc", "weaver")
)

type kubeadmInstaller struct {
	*baseInstaller
}

func NewKubeadmInstaller(opts ...InstallerOption) (Software, error) {
	bi, err := newBaseInstaller("kubeadm", opts...)
	if err != nil {
		return nil, err
	}

	return &kubeadmInstaller{
		baseInstaller: bi,
	}, nil
}

// Install installs the kubeadm binary and configuration files in the sandbox folder
func (ki *kubeadmInstaller) Install() error {
	// Install the kubeadm binary using the common logic
	err := ki.baseInstaller.Install()
	if err != nil {
		return err
	}

	// Install the kubeadm configuration files
	sandboxKubeletServiceDir := path.Join(core.Paths().SandboxDir, kubeletServiceDirRelPath)
	err = ki.installConfig(sandboxKubeletServiceDir)
	if err != nil {
		return err
	}

	return nil
}

// Configure configures the kubeadm binary,
// creates a copy of 10-kubeadm.conf with updated paths (.latest) and creates symlink for kubelet service directory
func (ki *kubeadmInstaller) Configure() error {
	// Create the symlink for the kubeadm binary
	err := ki.baseInstaller.Configure()
	if err != nil {
		return err
	}

	// Create the latest 10-kubeadm.conf file with updated paths
	err = ki.patchKubeadmConf()
	if err != nil {
		return err
	}

	// Create symlink for kubelet service directory
	err = ki.createKubeletServiceDirSymlink()
	if err != nil {
		return err
	}

	err = ki.configureKubeadmInit(ki.versionToBeInstalled)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to configure kubeadm init")
	}

	return nil
}

// Uninstall removes the kubeadm binary and configuration files from the sandbox folder
func (ki *kubeadmInstaller) Uninstall() error {
	err := ki.baseInstaller.Uninstall()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall kubeadm binary")
	}

	// Remove the kubeadm configuration files
	sandboxKubeletServiceDir := path.Join(core.Paths().SandboxDir, kubeletServiceDirRelPath)
	err = ki.baseInstaller.uninstallConfig(sandboxKubeletServiceDir)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall kubeadm configuration files from %s", sandboxKubeletServiceDir)
	}

	return nil
}

// RemoveConfiguration restores the kubeadm binary and configuration files to their original state
func (ki *kubeadmInstaller) RemoveConfiguration() error {
	err := ki.baseInstaller.RemoveConfiguration()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to restore kubeadm binary configuration")
	}

	// Remove the latest 10-kubeadm.conf file
	latestConfPath := getLatestPath(ki.getKubeadmConfPath())
	err = ki.fileManager.RemoveAll(latestConfPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove latest configuration file at %s", latestConfPath)
	}

	// Remove the symlink for the 10-kubeadm.conf file
	systemConfPath := path.Join(rootPath, kubeletServiceDirRelPath, kubeadmConfFileName)
	err = ki.fileManager.RemoveAll(systemConfPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove symlink for 10-kubeadm.conf file at %s", systemConfPath)
	}

	// Remove kubeadm-init.yaml configuration file
	initConfigPath := ki.getKubeadmInitConfigPath()
	err = ki.fileManager.RemoveAll(initConfigPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove kubeadm-init.yaml file at %s", initConfigPath)
	}

	return nil
}

// configureKubeadmInit generates the kubeadm init configuration file
// It retrieves the machine IP, generates a kubeadm token, and gets the hostname
// It then renders the kubeadm-init.yaml template with the retrieved values
func (ki *kubeadmInstaller) configureKubeadmInit(kubernetesVersion string) error {
	machineIp, err := network.GetMachineIP()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get machine IP address")
	}

	kubeadmToken, err := GenerateKubeadmToken()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to generate kubeadm token")
	}

	hostname, err := os.Hostname()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get hostname")
	}

	tmplData := templates.KubeadmInitData{
		KubeBootstrapToken: kubeadmToken,
		SandboxDir:         core.Paths().SandboxDir,
		MachineIP:          machineIp,
		Hostname:           hostname,
		KubernetesVersion:  kubernetesVersion,
	}

	rendered, err := templates.Render("files/kubeadm/kubeadm-init.yaml", tmplData)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render kubeadm init configuration template")
	}

	sandboxEtcWeaverDir := path.Join(core.Paths().SandboxDir, etcWeaverDirRelPath)

	err = ki.fileManager.CreateDirectory(sandboxEtcWeaverDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create directory for kubeadm init configuration")
	}
	err = ki.fileManager.WriteFile(path.Join(sandboxEtcWeaverDir, kubeadmInitConfigFileName), []byte(rendered))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write kubeadm init configuration file")
	}

	return nil
}

// GenerateKubeadmToken generates a random kubeadm token in the format [a-z0-9]{6}.[a-z0-9]{16}
var GenerateKubeadmToken = func() (string, error) {
	const allowedChars = "abcdefghijklmnopqrstuvwxyz0123456789"
	const part1Len = 6
	const part2Len = 16
	tokenPart := func(length int) (string, error) {
		b := make([]byte, length)
		for i := range b {
			nBig, err := rand.Int(rand.Reader, big.NewInt(int64(len(allowedChars))))
			if err != nil {
				return "", fmt.Errorf("failed to generate random int for kubeadm token: %w", err)
			}
			b[i] = allowedChars[nBig.Int64()]
		}
		return string(b), nil
	}
	part1, err := tokenPart(part1Len)
	if err != nil {
		return "", err
	}
	part2, err := tokenPart(part2Len)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%s", part1, part2), nil
}

// Overrides IsInstalled() to check if both binaries and config files are installed
func (ki *kubeadmInstaller) IsInstalled() (bool, error) {
	binariesInstalled, err := ki.baseInstaller.IsInstalled()
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if kubeadm binaries are installed")
	}

	sandboxKubeletServiceDir := path.Join(core.Paths().SandboxDir, kubeletServiceDirRelPath)
	configsInstalled, err := ki.isConfigInstalled(sandboxKubeletServiceDir)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if kubeadm configuration files are installed in %s", sandboxKubeletServiceDir)
	}

	return binariesInstalled && configsInstalled, nil
}

// Overrides IsConfigured() to check if the kubeadm is properly configured.
// This includes checking if the binaries are configured, the 10-kubeadm.conf file is properly updated,
// the systemd symlink for kubelet service directory is present, and kubeadm-init.yaml exists.
func (ki *kubeadmInstaller) IsConfigured() (bool, error) {
	// First check if binaries are configured
	binariesConfigured, err := ki.baseInstaller.IsConfigured()
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if kubeadm binaries are configured")
	}
	if !binariesConfigured {
		return false, nil
	}

	// Check if the .latest 10-kubeadm.conf file is valid
	latestConfValid, err := ki.isLatestKubeadmConfValid()
	if err != nil {
		return false, err
	}
	if !latestConfValid {
		return false, nil
	}

	// Check if the systemd symlink for kubelet service directory is present
	symlinkPresent, err := ki.isKubeletServiceDirSymlinkPresent()
	if err != nil {
		return false, err
	}
	if !symlinkPresent {
		return false, nil
	}

	// Check if kubeadm-init.yaml configuration file exists
	initConfigExists, err := ki.isKubeadmInitConfigExists()
	if err != nil {
		return false, err
	}

	return initConfigExists, nil
}

// isLatestKubeadmConfValid checks if the 10-kubeadm.conf file in the latest subfolder exists and has the correct content
func (ki *kubeadmInstaller) isLatestKubeadmConfValid() (bool, error) {
	latestConfPath := getLatestPath(ki.getKubeadmConfPath())

	// Check if the file in latest subfolder exists
	fi, exists, err := ki.fileManager.PathExists(latestConfPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if latest 10-kubeadm.conf exists at %s", latestConfPath)
	}
	if !exists || !ki.fileManager.IsRegularFileByFileInfo(fi) {
		return false, nil
	}

	// Read the original 10-kubeadm.conf file to create expected content
	originalConfPath := ki.getKubeadmConfPath()
	originalContent, err := ki.fileManager.ReadFile(originalConfPath, -1)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read original 10-kubeadm.conf file at %s", originalConfPath)
	}

	// Generate expected content with replaced paths
	sandboxKubeletPath := path.Join(core.Paths().SandboxBinDir, "kubelet")
	expectedContent := strings.ReplaceAll(string(originalContent), "/usr/bin/kubelet", sandboxKubeletPath)

	// Read the actual latest file content
	actualContent, err := ki.fileManager.ReadFile(latestConfPath, -1)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to read latest 10-kubeadm.conf file at %s", latestConfPath)
	}

	// Compare contents
	return string(actualContent) == expectedContent, nil
}

// isKubeletServiceDirSymlinkPresent checks if the 10-kubeadm.conf symlink exists in the system directory
func (ki *kubeadmInstaller) isKubeletServiceDirSymlinkPresent() (bool, error) {
	systemConfPath := path.Join(rootPath, kubeletServiceDirRelPath, kubeadmConfFileName)
	_, exists, err := ki.fileManager.PathExists(systemConfPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if 10-kubeadm.conf symlink exists at %s", systemConfPath)
	}

	return exists, nil
}

// isKubeadmInitConfigExists checks if the kubeadm-init.yaml configuration file exists
func (ki *kubeadmInstaller) isKubeadmInitConfigExists() (bool, error) {
	kubeadmInitPath := path.Join(core.Paths().SandboxDir, etcWeaverDirRelPath, kubeadmInitConfigFileName)

	fi, exists, err := ki.fileManager.PathExists(kubeadmInitPath)
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to check if kubeadm-init.yaml exists at %s", kubeadmInitPath)
	}

	return exists && ki.fileManager.IsRegularFileByFileInfo(fi), nil
}

// getKubeadmConfPath returns the path to the 10-kubeadm.conf file in the sandbox
func (ki *kubeadmInstaller) getKubeadmConfPath() string {
	return path.Join(core.Paths().SandboxDir, kubeletServiceDirRelPath, kubeadmConfFileName)
}

// getKubeadmInitConfigPath returns the path to the kubeadm-init.yaml configuration file
func (ki *kubeadmInstaller) getKubeadmInitConfigPath() string {
	return path.Join(core.Paths().SandboxDir, etcWeaverDirRelPath, kubeadmInitConfigFileName)
}

// patchKubeadmConf creates a copy of 10-kubeadm.conf with updated paths in the latest subfolder
func (ki *kubeadmInstaller) patchKubeadmConf() error {
	originalConfPath := ki.getKubeadmConfPath()
	latestConfPath := getLatestPath(originalConfPath)

	// Create latest subfolder if it doesn't exist
	latestDir := path.Dir(latestConfPath)
	err := ki.fileManager.CreateDirectory(latestDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create latest subfolder at %s", latestDir)
	}

	// Create latest file which will have some strings replaced
	err = ki.fileManager.CopyFile(originalConfPath, latestConfPath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create latest 10-kubeadm.conf file at %s", latestConfPath)
	}

	// Replace the kubelet binary path with sandbox path
	sandboxKubeletPath := path.Join(core.Paths().SandboxBinDir, "kubelet")
	err = ki.replaceAllInFile(latestConfPath, "/usr/bin/kubelet", sandboxKubeletPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace kubelet path in 10-kubeadm.conf file")
	}

	return nil
}

// createKubeletServiceDirSymlink creates a symlink for the 10-kubeadm.conf file
func (ki *kubeadmInstaller) createKubeletServiceDirSymlink() error {
	// Create the target directory if it doesn't exist
	kubeletServiceDir := path.Join(rootPath, kubeletServiceDirRelPath)

	err := ki.fileManager.CreateDirectory(kubeletServiceDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create kubelet service directory at %s", kubeletServiceDir)
	}

	// Create symlink from the .latest file to the system location
	latestConfPath := getLatestPath(ki.getKubeadmConfPath())
	systemConfPath := path.Join(kubeletServiceDir, kubeadmConfFileName)

	err = ki.fileManager.CreateSymbolicLink(latestConfPath, systemConfPath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create symlink for 10-kubeadm.conf from %s to %s", latestConfPath, systemConfPath)
	}

	return nil
}
