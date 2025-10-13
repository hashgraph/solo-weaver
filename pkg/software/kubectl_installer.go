package software

type kubectlInstaller struct {
	*BaseInstaller
}

var _ Software = (*kubectlInstaller)(nil)

func NewKubectlInstaller(softwareVersion string) (Software, error) {
	baseInstaller, err := NewBaseInstaller("kubectl", softwareVersion)
	if err != nil {
		return nil, err
	}

	return &kubectlInstaller{
		BaseInstaller: baseInstaller,
	}, nil
}

func (ki *kubectlInstaller) Download() error {
	return ki.BaseInstaller.Download()
}

func (ki *kubectlInstaller) Extract() error {
	// kubectl is typically a single binary, no extraction needed
	return nil
}

func (ki *kubectlInstaller) Install() error {
	return ki.BaseInstaller.Install()
}

func (ki *kubectlInstaller) Verify() error {
	return ki.BaseInstaller.Verify()
}

func (ki *kubectlInstaller) IsInstalled() (bool, error) {
	return ki.BaseInstaller.IsInstalled()
}

func (ki *kubectlInstaller) Configure() error {
	// kubectl-specific configuration logic
	return nil
}

func (ki *kubectlInstaller) IsConfigured() (bool, error) {
	return false, nil
}
