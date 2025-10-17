package software

func NewKubectlInstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("kubectl", opts...)
}
