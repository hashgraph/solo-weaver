package platform

const DefaultInstallRootPath = "/opt/hgcapp"

type Manager interface {
	// InstallFilesFromPackage installs the files from the package into the install path.
	// extractedSDKPath is the path to the extracted SDK.
	// extractedSDKDataPath is the path to the extracted SDK data.
	// isUpgrade is true if this is an upgrade, false if it is a new install.
	InstallFilesFromPackage(extractedSDKPath string, extractedSDKDataPath string, isUpgrade bool) error
	// GetInstallRootPath returns the root path of the installation as long as it is not sitting in a tmp folder.
	// If it is in a tmp folder, it returns the default install root path.
	// If the executable path is /opt/hgcapp/solo-provisioner/bin, then the install root path is /opt/hgcapp
	// If an error occurs while trying to get the install path it will return the error.
	GetInstallRootPath() (string, error)
}
