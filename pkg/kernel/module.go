package kernel

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"

	"pault.ag/go/modprobe"
)

const (
	// modulesLoadDirPath is the systemd modules-load.d directory path pattern
	modulesLoadDirPath = "/etc/modules-load.d/%s.conf"
)

// defaultModule is the default implementation of the Module interface
type defaultModule struct {
	name string
	ops  moduleOperations
}

// defaultOperations is the default implementation of moduleOperations
type defaultOperations struct{}

func NewModule(name string) (Module, error) {
	return &defaultModule{
		name: name,
		ops:  &defaultOperations{},
	}, nil
}

// defaultModule implements the Module interface
var _ Module = (*defaultModule)(nil)

func (m *defaultModule) Name() string {
	return m.name
}

func (m *defaultModule) IsLoaded() (loaded bool, persisted bool, err error) {
	isLoaded, err := m.ops.isLoaded(m.name)
	if err != nil {
		return false, false, err
	}

	isPersisted, err := m.ops.isPersisted(m.name)
	if err != nil {
		return isLoaded, false, err
	}

	return isLoaded, isPersisted, nil
}

func (m *defaultModule) Load(persist bool) error {
	isLoaded, err := m.ops.isLoaded(m.name)
	if err != nil {
		return err
	}

	if !isLoaded {
		err := m.ops.load(m.name)
		if err != nil {
			return err
		}
	}

	if persist {
		if err := m.ops.persist(m.name); err != nil {
			return err
		}
	}

	return nil
}

func (m *defaultModule) Unload(unpersist bool) error {
	if unpersist {
		if err := m.ops.unpersist(m.name); err != nil {
			return err
		}
	}

	isLoaded, err := m.ops.isLoaded(m.name)
	if err != nil {
		return err
	}
	if isLoaded {
		if err := m.ops.unload(m.name); err != nil {
			return err
		}
	}

	return nil
}

// defaultOperations implements the ModuleOperations interface
var _ moduleOperations = (*defaultOperations)(nil)

func (ops *defaultOperations) load(name string) error {
	return modprobe.Load(name, "")
}

func (ops *defaultOperations) unload(name string) error {
	return modprobe.Remove(name)
}

// persist ensures the module is loaded at boot time
func (ops *defaultOperations) persist(name string) error {
	persisted, err := ops.isPersisted(name)
	if err != nil {
		return err
	}

	if persisted {
		return nil
	}

	content := fmt.Sprintf("%s\n", name)
	return os.WriteFile(fmt.Sprintf(modulesLoadDirPath, name), []byte(content), 0640)
}

// unpersist removes the module from being loaded at boot time
func (ops *defaultOperations) unpersist(name string) error {
	confPath := fmt.Sprintf(modulesLoadDirPath, name)
	err := os.Remove(confPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// isLoaded checks if the module is currently loaded
func (ops *defaultOperations) isLoaded(name string) (bool, error) {
	// Method 1: sysfs directory exists
	if _, err := os.Stat("/sys/module/" + name); err == nil {
		return true, nil
	}

	// Method 2: stream /proc/modules and check line prefix "<name> "
	f, err := os.Open("/proc/modules")
	if err != nil {
		return false, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	prefix := []byte(name + " ")

	for sc.Scan() {
		line := sc.Bytes()
		if bytes.HasPrefix(line, prefix) {
			return true, nil
		}
	}
	if err := sc.Err(); err != nil {
		return false, err
	}

	return false, nil
}

// isPersisted checks if the module is configured to be loaded at boot
func (ops *defaultOperations) isPersisted(name string) (bool, error) {
	confPath := fmt.Sprintf(modulesLoadDirPath, name)

	file, err := os.Open(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == name {
			return true, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}

	return false, nil
}
