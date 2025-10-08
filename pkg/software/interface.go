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

type Software interface {
	Download() error

	Extract() error

	Install() error

	Verify() error

	IsInstalled() (bool, error)

	Configure() error

	IsConfigured() (bool, error)
}
