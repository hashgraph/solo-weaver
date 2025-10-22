package software

const CrioServiceName = "crio"

func NewCrioInstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("cri-o", opts...)
}
