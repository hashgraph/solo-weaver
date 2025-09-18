package container

type Networking interface {
	Install() error
	Uninstall() error
}
