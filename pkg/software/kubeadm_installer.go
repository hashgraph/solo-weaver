// SPDX-License-Identifier: Apache-2.0

package software

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/network"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
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
	err := ki.baseInstaller.performInstall()
	if err != nil {
		return err
	}

	// Install the kubeadm configuration files
	sandboxKubeletServiceDir := path.Join(core.Paths().SandboxDir, kubeletServiceDirRelPath)
	err = ki.installConfig(sandboxKubeletServiceDir)
	if err != nil {
		return err
	}

	// Record installed state
	_ = ki.GetStateManager().RecordState(ki.GetSoftwareName(), state.TypeInstalled, ki.Version())

	return nil
}

// Configure configures the kubeadm binary,
// creates a copy of 10-kubeadm.conf with updated paths (.latest) and creates symlink for kubelet service directory
func (ki *kubeadmInstaller) Configure() error {
	// Create the symlink for the kubeadm binary
	err := ki.baseInstaller.performConfiguration()
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

	// Record configured state
	_ = ki.GetStateManager().RecordState(ki.GetSoftwareName(), state.TypeConfigured, ki.Version())

	return nil
}

// Uninstall removes the kubeadm binary and configuration files from the sandbox folder
func (ki *kubeadmInstaller) Uninstall() error {
	// Uninstall the kubeadm binary using the common logic
	err := ki.baseInstaller.performUninstall()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall kubeadm binary")
	}

	// Remove the kubeadm configuration files
	sandboxKubeletServiceDir := path.Join(core.Paths().SandboxDir, kubeletServiceDirRelPath)
	err = ki.baseInstaller.uninstallConfig(sandboxKubeletServiceDir)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to uninstall kubeadm configuration files from %s", sandboxKubeletServiceDir)
	}

	// Remove installed state
	_ = ki.GetStateManager().RemoveState(ki.GetSoftwareName(), state.TypeInstalled)

	return nil
}

// RemoveConfiguration restores the kubeadm binary and configuration files to their original state
func (ki *kubeadmInstaller) RemoveConfiguration() error {
	// Remove the symlink for the 10-kubeadm.conf file
	systemConfPath := path.Join(rootPath, kubeletServiceDirRelPath, kubeadmConfFileName)
	err := ki.fileManager.RemoveAll(systemConfPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove symlink for 10-kubeadm.conf file at %s", systemConfPath)
	}

	// Remove kubeadm-init.yaml configuration file
	initConfigPath := ki.getKubeadmInitConfigPath()
	err = ki.fileManager.RemoveAll(initConfigPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove kubeadm-init.yaml file at %s", initConfigPath)
	}

	// Call base implementation to cleanup symlinks
	err = ki.baseInstaller.performConfigurationRemoval()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to restore kubeadm binary configuration")
	}

	// Remove configured state
	_ = ki.GetStateManager().RemoveState(ki.GetSoftwareName(), state.TypeConfigured)

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

// getKubeadmConfPath returns the path to the 10-kubeadm.conf file in the sandbox
func (ki *kubeadmInstaller) getKubeadmConfPath() string {
	return path.Join(core.Paths().SandboxDir, kubeletServiceDirRelPath, kubeadmConfFileName)
}

// getKubeadmInitConfigPath returns the path to the kubeadm-init.yaml configuration file
func (ki *kubeadmInstaller) getKubeadmInitConfigPath() string {
	return path.Join(core.Paths().SandboxDir, etcWeaverDirRelPath, kubeadmInitConfigFileName)
}

// patchKubeadmConf patches 10-kubeadm.conf with updated paths in place
func (ki *kubeadmInstaller) patchKubeadmConf() error {
	confPath := ki.getKubeadmConfPath()

	// Replace the kubelet binary path with sandbox path
	sandboxKubeletPath := path.Join(core.Paths().SandboxBinDir, "kubelet")
	err := ki.replaceAllInFile(confPath, "/usr/bin/kubelet", sandboxKubeletPath)
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

	// Create symlink from the conf file to the system location
	confPath := ki.getKubeadmConfPath()
	systemConfPath := path.Join(kubeletServiceDir, kubeadmConfFileName)

	err = ki.fileManager.CreateSymbolicLink(confPath, systemConfPath, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create symlink for 10-kubeadm.conf from %s to %s", confPath, systemConfPath)
	}

	return nil
}
