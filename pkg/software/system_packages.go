package software

import "github.com/bluet/syspkg/manager"

func NewIptables() (Package, error) {
	return NewPackageInstaller(WithPackageName("iptables"), WithPackageOptions(manager.Options{AssumeYes: true}))
}

func NewGnupg2() (Package, error) {
	return NewPackageInstaller(WithPackageName("gnupg2"), WithPackageOptions(manager.Options{AssumeYes: true}))
}
