package software

import "github.com/bluet/syspkg/manager"

func NewIptables() (Package, error) {
	return NewPackageInstaller(WithPackageName("iptables"), WithPackageOptions(manager.Options{AssumeYes: true}))
}

// NewGpg creates a GPG package installer that works across different distributions
// Uses "gpg" package name which works on most modern distributions including:
// - Ubuntu/Debian (newer versions)
// - RHEL/CentOS/Fedora/Oracle Linux
// The underlying syspkg library automatically detects the correct package manager
func NewGpg() (Package, error) {
	return NewPackageInstaller(WithPackageName("gpg"), WithPackageOptions(manager.Options{AssumeYes: true}))
}

func NewConntrack() (Package, error) {
	return NewPackageInstaller(WithPackageName("conntrack"), WithPackageOptions(manager.Options{AssumeYes: true}))
}

func NewSocat() (Package, error) {
	return NewPackageInstaller(WithPackageName("socat"), WithPackageOptions(manager.Options{AssumeYes: true}))
}

func NewEbtables() (Package, error) {
	return NewPackageInstaller(WithPackageName("ebtables"), WithPackageOptions(manager.Options{AssumeYes: true}))
}

func NewNftables() (Package, error) {
	return NewPackageInstaller(WithPackageName("nftables"), WithPackageOptions(manager.Options{AssumeYes: true}))
}

func NewContainerd() (Package, error) {
	return NewPackageInstaller(WithPackageName("containerd"), WithPackageOptions(manager.Options{AssumeYes: true}))
}
