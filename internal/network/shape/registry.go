// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joomcode/errorx"
)

// devicePath returns the on-disk path of a device's config file.
func devicePath(dir string) string {
	return filepath.Join(DeviceConfigDir, dir+".json")
}

// classPath returns the on-disk path of a class's config file.
func classPath(name string) string {
	return filepath.Join(ClassConfigDir, name+".json")
}

// writeConfigJSON atomically writes v as indented JSON to path, first creating
// its parent directory. Shared by writeDevice/writeClass.
func writeConfigJSON(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create config dir %s", dir)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal config %s", path)
	}
	return atomicWriteFile(path, string(data)+"\n", 0o644)
}

// readConfigJSON loads a *T from path, returning (nil, nil) when the file does
// not exist. Shared by readDevice/readClass.
func readConfigJSON[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errorx.ExternalError.Wrap(err, "failed to read config %s", path)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse config %s", path)
	}
	return &v, nil
}

// writeDevice atomically writes the device config JSON.
func writeDevice(dev *DeviceConfig) error { return writeConfigJSON(devicePath(dev.Dir), dev) }

// writeClass atomically writes the class config JSON.
func writeClass(cls *ClassConfig) error { return writeConfigJSON(classPath(cls.Name), cls) }

// readDevice loads a device config by dir, returning nil if not found.
func readDevice(dir string) (*DeviceConfig, error) {
	return readConfigJSON[DeviceConfig](devicePath(dir))
}

// readClass loads a class config by name, returning nil if not found.
func readClass(name string) (*ClassConfig, error) {
	return readConfigJSON[ClassConfig](classPath(name))
}

// loadClassesForDir loads all class configs for the given direction, sorted by
// name. Classes whose name is not in classInfoMap (e.g. hand-edited files) are
// silently skipped.
func loadClassesForDir(dir string) ([]*ClassConfig, error) {
	entries, err := os.ReadDir(ClassConfigDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errorx.ExternalError.Wrap(err, "failed to read class config dir %s", ClassConfigDir)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		info, ok := classInfoMap[name]
		if !ok || info.Dir != dir {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	classes := make([]*ClassConfig, 0, len(names))
	for _, n := range names {
		cls, err := readClass(n)
		if err != nil {
			return nil, err
		}
		if cls != nil {
			classes = append(classes, cls)
		}
	}
	return classes, nil
}

// removeConfigFile deletes path, ignoring not-found. Shared by
// removeDevice/removeClass.
func removeConfigFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return errorx.ExternalError.Wrap(err, "failed to remove config %s", path)
	}
	return nil
}

// removeClass deletes a class config file, ignoring not-found.
func removeClass(name string) error { return removeConfigFile(classPath(name)) }

// removeDevice deletes a device config file, ignoring not-found.
func removeDevice(dir string) error { return removeConfigFile(devicePath(dir)) }

// policyStampRef is a minimal representation of a policy registry entry used
// only to check stamp references when deleting a shape class. Avoids importing
// internal/network/policy.
type policyStampRef struct {
	Stamp      string `json:"stamp"`
	ReplyStamp string `json:"reply_stamp"`
}

// loadPolicyStamps reads the stamp and reply_stamp fields from every policy
// JSON in the policy registry, for cross-package delete validation. Returns a
// map from class name → slice of policy names that reference it.
func loadPolicyStamps() (map[string][]string, error) {
	entries, err := os.ReadDir(policyRegistryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errorx.ExternalError.Wrap(err, "failed to read policy registry %s", policyRegistryDir)
	}
	refs := make(map[string][]string)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		policyName := strings.TrimSuffix(e.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(policyRegistryDir, e.Name()))
		if err != nil {
			return nil, errorx.ExternalError.Wrap(err, "failed to read policy file %s", e.Name())
		}
		var ref policyStampRef
		if err := json.Unmarshal(data, &ref); err != nil {
			return nil, errorx.IllegalFormat.Wrap(err, "failed to parse policy file %s", e.Name())
		}
		if ref.Stamp != "" {
			refs[ref.Stamp] = append(refs[ref.Stamp], policyName)
		}
		if ref.ReplyStamp != "" {
			refs[ref.ReplyStamp] = append(refs[ref.ReplyStamp], policyName)
		}
	}
	return refs, nil
}
