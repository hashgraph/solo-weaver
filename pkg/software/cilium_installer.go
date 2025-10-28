package software

import (
	"path"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/network"
	"golang.hedera.com/solo-provisioner/internal/templates"
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
	machineIp, err := network.GetMachineIP()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get machine IP address")
	}

	// create anonymous struct for template data
	tmplData := struct {
		SandboxDir string
		MachineIP  string
	}{
		SandboxDir: core.Paths().SandboxDir,
		MachineIP:  machineIp,
	}

	rendered, err := templates.Render("files/cilium/cilium-config.yaml", tmplData)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render cilium configuration template")
	}

	configurationDir := path.Join(core.Paths().SandboxDir, "etc", "provisioner")
	// Create the configuration directory if it doesn't exist
	err = ci.fileManager.CreateDirectory(configurationDir, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create cilium configuration directory")
	}

	// write the configuration file
	err = ci.fileManager.WriteFile(path.Join(configurationDir, "cilium-config.yaml"), []byte(rendered))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write cilium configuration file")
	}

	return nil
}
