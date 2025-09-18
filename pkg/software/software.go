package software

type Package interface {
	Install() error
	Uninstall() error
	Upgrade() error
	IsInstalled() (bool, error)
	Version() (string, error)
	Verify() error
}
