package container

type Runtime interface {
	Install() error
	Uninstall() error
}
