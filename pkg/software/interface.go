package software

import "github.com/bluet/syspkg"

type Package interface {
	Name() string
	Install() (*syspkg.PackageInfo, error)
	Uninstall() (*syspkg.PackageInfo, error)
	Upgrade() (*syspkg.PackageInfo, error)
	Info() (*syspkg.PackageInfo, error)
	Verify() error
	IsInstalled() bool
}
