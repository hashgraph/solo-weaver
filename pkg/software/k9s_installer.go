// SPDX-License-Identifier: Apache-2.0

package software

const K9sBinaryName = "k9s"

func NewK9sInstaller(opts ...InstallerOption) (Software, error) {
	return newBaseInstaller("k9s", opts...)
}
