package software

func NewCrioInstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("cri-o", opts...)
}
