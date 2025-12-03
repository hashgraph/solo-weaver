// SPDX-License-Identifier: Apache-2.0

package software

func NewHelmInstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("helm", opts...)
}
