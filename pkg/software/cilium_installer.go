// SPDX-License-Identifier: Apache-2.0

package software

import (
	"path"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/network"
	"golang.hedera.com/solo-weaver/internal/templates"
)

const (
	ciliumConfigFileName = "cilium-config.yaml"
	ciliumTemplateFile   = "files/cilium/cilium-config.yaml"
)

type ciliumInstaller struct {
	*baseInstaller
}

// NewCiliumInstaller creates a new installer for Cilium
func NewCiliumInstaller(opts ...InstallerOption) (Software, error) {
	bi, err := newBaseInstaller("cilium", opts...)
	if err != nil {
		return nil, err
	}

	return &ciliumInstaller{
		baseInstaller: bi,
	}, nil
}

// Configure configures the cilium after installation
func (ci *ciliumInstaller) Configure() error {
	// Create the symlink for the cilium binary
	err := ci.baseInstaller.Configure()
	if err != nil {
		return err
	}

	// Setup cilium configuration file
	err = ci.createCiliumConfigFile()
	if err != nil {
		return err
	}

	return nil
}

// RemoveConfiguration restores the cilium binary and configuration files to their original state
func (ci *ciliumInstaller) RemoveConfiguration() error {
	// Remove cilium-config.yaml configuration file
	configPath := ci.getCiliumConfigPath()
	err := ci.fileManager.RemoveAll(configPath)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to remove cilium-config.yaml file at %s", configPath)
	}

	// Call base implementation to cleanup symlinks
	err = ci.baseInstaller.RemoveConfiguration()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to restore cilium binary configuration")
	}

	return nil
}

// getCiliumConfigPath returns the path to the cilium-config.yaml configuration file
func (ci *ciliumInstaller) getCiliumConfigPath() string {
	return path.Join(ci.getConfigurationDir(), ciliumConfigFileName)
}

// getConfigurationDir returns the path to the weaver configuration directory
func (ci *ciliumInstaller) getConfigurationDir() string {
	return path.Join(core.Paths().SandboxDir, "etc", "weaver")
}

// createCiliumConfigFile creates the cilium configuration file from template
func (ci *ciliumInstaller) createCiliumConfigFile() error {
	machineIp, err := network.GetMachineIP()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get machine IP address")
	}

	tmplData := struct {
		SandboxDir string
		MachineIP  string
	}{
		SandboxDir: core.Paths().SandboxDir,
		MachineIP:  machineIp,
	}

	rendered, err := templates.Render(ciliumTemplateFile, tmplData)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render cilium configuration template")
	}

	configurationDir := ci.getConfigurationDir()
	// Create the configuration directory if it doesn't exist
	err = ci.fileManager.CreateDirectory(configurationDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create cilium configuration directory")
	}

	// write the configuration file
	configFilePath := ci.getCiliumConfigPath()
	err = ci.fileManager.WriteFile(configFilePath, []byte(rendered))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write cilium configuration file")
	}

	return nil
}
