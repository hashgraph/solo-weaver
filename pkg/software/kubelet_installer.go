package software

import (
	"path"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
)

type kubeletInstaller struct {
	*BaseInstaller
}

var _ Software = (*kubeletInstaller)(nil)

func NewKubeletInstaller() (Software, error) {
	baseInstaller, err := NewBaseInstaller("kubelet", "")
	if err != nil {
		return nil, err
	}

	return &kubeletInstaller{
		BaseInstaller: baseInstaller,
	}, nil
}

func (ki *kubeletInstaller) Download() error {
	// Download kubeadm binary using base implementation
	if err := ki.BaseInstaller.Download(); err != nil {
		return err
	}

	// Download kubelet configuration files (specific to kubelet)
	if err := ki.downloadKubeletConfigFiles(); err != nil {
		return err
	}

	return nil
}

func (ki *kubeletInstaller) downloadKubeletConfigFiles() error {
	downloadFolder := ki.DownloadFolder()
	metadata := ki.Software()
	fileManager := ki.FileManager()
	downloader := ki.Downloader()

	// Get config files for kubeadm from the software configuration
	configs, err := metadata.GetConfigs(ki.Version())
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

func (ki *kubeletInstaller) Extract() error {
	// kubelet is typically a single binary, no extraction needed
	return nil
}

func (ki *kubeletInstaller) Install() error {
	// Install the kubeadm binary using the common logic
	err := ki.BaseInstaller.Install()
	if err != nil {
		return err
	}

	// Install the kubelet configuration files
	err = ki.BaseInstaller.InstallConfigFiles(path.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir))
	if err != nil {
		return err
	}

	return nil
}

func (ki *kubeletInstaller) Verify() error {
	return ki.BaseInstaller.Verify()
}

func (ki *kubeletInstaller) IsInstalled() (bool, error) {
	return ki.BaseInstaller.IsInstalled()
}

func (ki *kubeletInstaller) Configure() error {
	fileManager := ki.FileManager()
	sandboxBinary := path.Join(core.Paths().SandboxBinDir, "kubelet")

	// Create symlink to /usr/local/bin for system-wide access
	systemBinary := path.Join(core.SystemBinDir, "kubelet")

	// Create new symlink
	err := fileManager.CreateSymbolicLink(sandboxBinary, systemBinary, true)
	if err != nil {
		return NewInstallationError(err, sandboxBinary, systemBinary)
	}

	// Replace strings in configuration file and create symlink

	sandboxKubeletServiceDir := path.Join(core.Paths().SandboxDir, core.SystemdUnitFilesDir)
	kubeadmConfDest := path.Join(sandboxKubeletServiceDir, "kubelet.service")

	err = ki.replaceAllInFile(kubeadmConfDest, "/usr/bin/kubelet", path.Join(core.Paths().SandboxBinDir, "kubelet"))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to replace kubelet path in kubelet.service")
	}

	err = fileManager.CreateSymbolicLink(kubeadmConfDest, path.Join(core.SystemdUnitFilesDir, "kubelet.service"), true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create symlink for kubelet service directory")
	}

	return nil
}

func (ki *kubeletInstaller) IsConfigured() (bool, error) {
	return false, nil
}
