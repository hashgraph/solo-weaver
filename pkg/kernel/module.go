package kernel

type Module interface {
	Load() error
	Unload() error
	IsLoaded() (bool, error)
}
