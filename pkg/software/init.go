// SPDX-License-Identifier: Apache-2.0

package software

type InstallerFunc func(opts ...InstallerOption) (Software, error)

var installerConstructors = map[string]InstallerFunc{
	CiliumBinaryName:   NewCiliumInstaller,
	CrioBinaryName:     NewCrioInstaller,
	HelmBinaryName:     NewHelmInstaller,
	K9sBinaryName:      NewK9sInstaller,
	KubeadmBinaryName:  NewKubeadmInstaller,
	KubectlBinaryName:  NewKubectlInstaller,
	KubeletBinaryName:  NewKubeletInstaller,
	TeleportBinaryName: NewTeleportNodeAgentInstaller,
}

// Installers returns a map of available installer constructors.
// The keys of the map are the names of the software, and the values are the corresponding installer functions.
// This allows for dynamic retrieval of installer functions based on software names, facilitating extensibility and modularity in the installation process.
// This is primarily used by the machineChecker to determine which installers are available for checking and installation on the local host.
func Installers() map[string]InstallerFunc {
	return installerConstructors
}
