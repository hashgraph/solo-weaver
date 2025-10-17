package software

func NewHelmInstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("helm", opts...)
}
