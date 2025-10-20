package software

// NewCiliumInstaller creates a new installer for Cilium CLI
// The installation of Cilium CNI plugin is not handled here, but rather
// through CiliumCNI package under internal
func NewCiliumInstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("cilium", opts...)
}
