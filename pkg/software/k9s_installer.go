package software

func NewK9sInstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("k9s", opts...)
}
