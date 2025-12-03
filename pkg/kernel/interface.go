// SPDX-License-Identifier: Apache-2.0

package kernel

// Module defines the interface for kernel module management operations
type Module interface {
	Load(persist bool) error
	Unload(unpersist bool) error
	IsLoaded() (loaded bool, persisted bool, err error)
	Name() string
}

// moduleOperations defines the low-level operations for kernel module management
// This interface can be easily mocked for testing
type moduleOperations interface {
	load(name string) error
	unload(name string) error
	persist(name string) error
	unpersist(name string) error
	isLoaded(name string) (bool, error)
	isPersisted(name string) (bool, error)
}
