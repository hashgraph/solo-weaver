package software

func NewCiliumnstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("cilium", opts...)
}
