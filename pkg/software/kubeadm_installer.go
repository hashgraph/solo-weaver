package software

import (
	"path"
	"strings"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
)

const (
	kubeletServiceDir = "/usr/lib/systemd/system/kubelet.service.d"
)

type kubeadmInstaller struct {
	*BaseInstaller
}

var _ Software = (*kubeadmInstaller)(nil)

func NewKubeadmInstaller(selectedVersion string) (Software, error) {
	baseInstaller, err := NewBaseInstaller("kubeadm", selectedVersion)
	if err != nil {
		return nil, err
	}

	return &kubeadmInstaller{
		BaseInstaller: baseInstaller,
	}, nil
}

// Download downloads the kubeadm binary and configuration files
func (ki *kubeadmInstaller) Download() error {
	// Download kubeadm binary using base implementation
	if err := ki.BaseInstaller.Download(); err != nil {
		return err
	}

	// Download kubeadm configuration files (specific to kubeadm)
	if err := ki.downloadKubeadmConfigFiles(); err != nil {
		return err
	}

	return nil
}

func (ki *kubeadmInstaller) downloadKubeadmConfigFiles() error {
	downloadFolder := ki.GetDownloadFolder()
	metadata := ki.GetMetadata()
	fileManager := ki.GetFileManager()
	downloader := ki.GetDownloader()

	// Get config files for kubeadm from the software configuration
	configs, err := metadata.GetConfigs(ki.GetVersion())
	if err != nil {
		return err
	}

	// Download the config file
	for _, config := range configs {
		configFile := path.Join(downloadFolder, config.Filename)

		// Check if file already exists and verify checksum
		_, exists, err := fileManager.PathExists(configFile)
		if err == nil && exists {
			// File exists, verify checksum
			if err := VerifyChecksum(configFile, config.Value, config.Algorithm); err == nil {
				// File is already downloaded and valid
				continue
			}
			// File exists but invalid checksum, remove it and re-download
			if err := fileManager.RemoveAll(configFile); err != nil {
				return err
			}
		}

		// Download the config file
		if err := downloader.Download(config.URL, configFile); err != nil {
			return err
		}

		// Verify the downloaded config file's checksum
		if err := VerifyChecksum(configFile, config.Value, config.Algorithm); err != nil {
			return err
		}
	}

	return nil
}

func (ki *kubeadmInstaller) Extract() error {
	// Kubeadm might not need extraction
	return nil
}

func (ki *kubeadmInstaller) Install() error {
	// Install the kubeadm binary using the common logic
	if err := ki.BaseInstaller.Install(); err != nil {
		return err
	}

	// Handle kubeadm-specific configuration file installation
	fileManager := ki.GetFileManager()
	metadata := ki.GetMetadata()

	// Verify that the config file exists
	configs, err := metadata.GetConfigs(ki.GetVersion())
	if err != nil {
		return err
	}
	if len(configs) == 0 {
		return NewFileNotFoundError("10-kubeadm.conf")
	}
	if len(configs) > 1 {
		return errorx.IllegalState.New("expected exactly one kubeadm config, but found %d", len(configs))
	}

	configFilename := configs[0].Filename

	// Install the configuration file to the sandbox
	sandboxKubeletServiceDir := path.Join(core.Paths().SandboxDir, kubeletServiceDir)
	err = fileManager.CreateDirectory(sandboxKubeletServiceDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create kubelet service directory in sandbox")
	}

	kubeAdmConfSrc := path.Join(ki.GetDownloadFolder(), configFilename)
	kubeadmConfDest := path.Join(sandboxKubeletServiceDir, configFilename)
	err = fileManager.CopyFile(kubeAdmConfSrc, kubeadmConfDest, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to copy 10-kubeadm.conf to sandbox")
	}

	return nil
}

func (ki *kubeadmInstaller) Verify() error {
	return ki.BaseInstaller.Verify()
}

func (ki *kubeadmInstaller) IsInstalled() (bool, error) {
	return ki.BaseInstaller.IsInstalled()
}

func (ki *kubeadmInstaller) Configure() error {
	fileManager := ki.GetFileManager()
	sandboxBinary := path.Join(core.Paths().SandboxBinDir, "kubeadm")

	// Create symlink to /usr/local/bin for system-wide access
	systemBinary := "/usr/local/bin/kubeadm"

	// Create new symlink
	err := fileManager.CreateSymbolicLink(sandboxBinary, systemBinary, true)
	if err != nil {
		return NewInstallationError(err, sandboxBinary, systemBinary)
	}

	sandboxKubeletServiceDir := path.Join(core.Paths().SandboxDir, kubeletServiceDir)
	kubeadmConfDest := path.Join(sandboxKubeletServiceDir, "10-kubeadm.conf")

	err = ki.replaceKubeletPath(kubeadmConfDest, path.Join(core.Paths().SandboxBinDir, "kubelet"))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace kubelet path in 10-kubeadm.conf")
	}

	err = fileManager.CreateSymbolicLink(sandboxKubeletServiceDir, kubeletServiceDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create symlink for kubelet service directory")
	}

	return nil
}

// replaceKubeletPath replaces the kubelet path in the kubeadm configuration file
func (ki *kubeadmInstaller) replaceKubeletPath(kubeadmFile string, newKubeletPath string) error {
	fileManager := ki.GetFileManager()

	input, err := fileManager.ReadFile(kubeadmFile, -1)
	if err != nil {
		return err
	}

	output := strings.ReplaceAll(string(input), "/usr/bin/kubelet", newKubeletPath)
	err = fileManager.WriteFile(kubeadmFile, []byte(output))
	if err != nil {
		return err
	}

	err = fileManager.WritePermissions(kubeadmFile, 0644, false)
	if err != nil {
		return err
	}

	return nil
}

func (ki *kubeadmInstaller) IsConfigured() (bool, error) {
	return false, nil
}
