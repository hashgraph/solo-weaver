package software

type crioInstaller struct {
	*BaseInstaller
}

var _ Software = (*crioInstaller)(nil)

func NewCrioInstaller() (Software, error) {
	return NewCrioInstallerWithVersion("")
}

func NewCrioInstallerWithVersion(selectedVersion string) (Software, error) {
	baseInstaller, err := NewBaseInstaller("cri-o", selectedVersion)
	if err != nil {
		return nil, err
	}

	return &crioInstaller{
		BaseInstaller: baseInstaller,
	}, nil
}

// Download uses the base implementation
func (ci *crioInstaller) Download() error {
	return ci.BaseInstaller.Download()
}

// Extract uses the base implementation
func (ci *crioInstaller) Extract() error {
	return ci.BaseInstaller.Extract()
}

func (ci *crioInstaller) Install() error {
	// CRI-O specific installation logic
	// mv to sandbox
	// installation/binary symlink
	return nil
}

func (ci *crioInstaller) Verify() error {
	return ci.BaseInstaller.Verify()
}

func (ci *crioInstaller) IsInstalled() (bool, error) {
	return ci.BaseInstaller.IsInstalled()
}

func (ci *crioInstaller) Configure() error {
	// CRI-O specific configuration logic
	// default configuration: /etc/default/crio
	// service configuration: /usr/lib/systemd/system/crio.service
	// application configuration: /etc/crio/crio.conf.d
	// configuration service symlink: /usr/lib/systemd/system/crio.service
	return nil
}

func (ci *crioInstaller) IsConfigured() (bool, error) {
	// Check default, service, application and configuration service symlinks
	return false, nil
}
