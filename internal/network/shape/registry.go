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

// writeDevice atomically writes the device config JSON.
func writeDevice(dev *DeviceConfig) error {
	if err := os.MkdirAll(DeviceConfigDir, 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create device config dir %s", DeviceConfigDir)
	}
	data, err := json.MarshalIndent(dev, "", "  ")
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal device config %q", dev.Dir)
	}
	return atomicWriteFile(devicePath(dev.Dir), string(data)+"\n", 0o644)
}

// writeClass atomically writes the class config JSON.
func writeClass(cls *ClassConfig) error {
	if err := os.MkdirAll(ClassConfigDir, 0o755); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create class config dir %s", ClassConfigDir)
	}
	data, err := json.MarshalIndent(cls, "", "  ")
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal class config %q", cls.Name)
	}
	return atomicWriteFile(classPath(cls.Name), string(data)+"\n", 0o644)
}

// readDevice loads a device config by dir, returning nil if not found.
func readDevice(dir string) (*DeviceConfig, error) {
	data, err := os.ReadFile(devicePath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errorx.ExternalError.Wrap(err, "failed to read device config %s", devicePath(dir))
	}
	var dev DeviceConfig
	if err := json.Unmarshal(data, &dev); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse device config %s", devicePath(dir))
	}
	return &dev, nil
}

// readClass loads a class config by name, returning nil if not found.
func readClass(name string) (*ClassConfig, error) {
	data, err := os.ReadFile(classPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errorx.ExternalError.Wrap(err, "failed to read class config %s", classPath(name))
	}
	var cls ClassConfig
	if err := json.Unmarshal(data, &cls); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse class config %s", classPath(name))
	}
	return &cls, nil
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

// loadAllClasses loads all class configs (any direction), sorted by name.
func loadAllClasses() ([]*ClassConfig, error) {
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
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
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

// removeClass deletes a class config file, ignoring not-found.
func removeClass(name string) error {
	err := os.Remove(classPath(name))
	if err != nil && !os.IsNotExist(err) {
		return errorx.ExternalError.Wrap(err, "failed to remove class config %s", classPath(name))
	}
	return nil
}

// removeDevice deletes a device config file, ignoring not-found.
func removeDevice(dir string) error {
	err := os.Remove(devicePath(dir))
	if err != nil && !os.IsNotExist(err) {
		return errorx.ExternalError.Wrap(err, "failed to remove device config %s", devicePath(dir))
	}
	return nil
}

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
