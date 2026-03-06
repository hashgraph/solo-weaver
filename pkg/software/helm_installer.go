// SPDX-License-Identifier: Apache-2.0

package software

const HelmBinaryName = "helm"

func NewHelmInstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("helm", opts...)
}
