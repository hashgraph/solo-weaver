package software

type Package interface {
	Install() error
	Uninstall() error
	Upgrade() error
	IsInstalled() (bool, error)
	Version() (string, error)
	Verify() error
}

type option func(*packageInstaller)

// packageInstaller is the default implementation of the Package interface
// that provides common functionality for installing, uninstalling, upgrading,
// checking installation status, getting version, and verifying the installation
// of software packages.
type packageInstaller struct {
	packageUrl     string
	packageHash    string
	packageVersion string
}

func (p *packageInstaller) Install() error {
	// Implementation for installing iptables
	return nil
}

func (p *packageInstaller) Uninstall() error {
	// Implementation for uninstalling iptables
	return nil
}

func (p *packageInstaller) Upgrade() error {
	// Implementation for upgrading iptables
	return nil
}

func (p *packageInstaller) IsInstalled() (bool, error) {
	// Implementation to check if iptables is installed
	return true, nil
}

func (p *packageInstaller) Version() (string, error) {
	// Implementation to get the version of iptables
	return "1.8.7", nil
}

func (p *packageInstaller) Verify() error {
	// Implementation to verify the installation of iptables
	return nil
}

func WithPackageUrl(url string) func(*packageInstaller) {
	return func(pb *packageInstaller) {
		pb.packageUrl = url
	}
}

func WithPackageHash(hash string) func(*packageInstaller) {
	return func(pb *packageInstaller) {
		pb.packageHash = hash
	}
}

func WithPackageVersion(version string) func(*packageInstaller) {
	return func(pb *packageInstaller) {
		pb.packageVersion = version
	}
}

func newPackageInstaller(opts ...option) *packageInstaller {
	pm := &packageInstaller{}
	for _, opt := range opts {
		opt(pm)
	}
	return pm
}
