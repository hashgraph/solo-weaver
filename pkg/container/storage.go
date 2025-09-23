package container

type Storage interface {
	Install() error
	Uninstall() error
}
