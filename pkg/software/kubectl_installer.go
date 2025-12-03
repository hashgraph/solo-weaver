// SPDX-License-Identifier: Apache-2.0

package software

func NewKubectlInstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("kubectl", opts...)
}
