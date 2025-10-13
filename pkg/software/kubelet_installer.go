package software

type kubeletInstaller struct {
	*BaseInstaller
}

var _ Software = (*kubeletInstaller)(nil)

func NewKubeletInstaller(selectedVersion string) (Software, error) {
	baseInstaller, err := NewBaseInstaller("kubelet", selectedVersion)
	if err != nil {
		return nil, err
	}

	return &kubeletInstaller{
		BaseInstaller: baseInstaller,
	}, nil
}

func (ki *kubeletInstaller) Download() error {
	return ki.BaseInstaller.Download()
}

func (ki *kubeletInstaller) Extract() error {
	// kubelet is typically a single binary, no extraction needed
	return nil
}

func (ki *kubeletInstaller) Install() error {
	return ki.BaseInstaller.Install()
}

func (ki *kubeletInstaller) Verify() error {
	return ki.BaseInstaller.Verify()
}

func (ki *kubeletInstaller) IsInstalled() (bool, error) {
	return ki.BaseInstaller.IsInstalled()
}

func (ki *kubeletInstaller) Configure() error {
	// kubelet-specific configuration logic
	return nil
}

func (ki *kubeletInstaller) IsConfigured() (bool, error) {
	return false, nil
}
