// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joomcode/errorx"
)

// registryPath returns the on-disk path of a policy's registry file.
func registryPath(dir, name string) string {
	return filepath.Join(dir, name+".json")
}

// writeEntry atomically writes a policy's registry JSON. The daemon poll loop
// never calls this — the registry is operator/CLI-owned.
func writeEntry(dir string, p *Policy) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal policy %q", p.Name)
	}
	// json.MarshalIndent omits the trailing newline; add one so the file is a
	// well-formed text file and round-trips cleanly through editors/diffs.
	return atomicWriteFile(registryPath(dir, p.Name), string(data)+"\n", 0o644)
}

// Exists reports whether a policy with the given name is present in the
// registry at RegistryDir. A missing file is not an error (returns false). It
// lets install orchestration tell a genuinely new policy from one that already
// existed before a create — e.g. so rollback deletes only what it created and
// never an operator-owned policy replaced via --force.
func Exists(name string) (bool, error) {
	_, err := os.Stat(registryPath(RegistryDir, name))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, errorx.ExternalError.Wrap(err, "failed to stat policy registry %s", registryPath(RegistryDir, name))
}

// readEntry loads a single policy from its registry file.
func readEntry(dir, name string) (*Policy, error) {
	data, err := os.ReadFile(registryPath(dir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // not an error: caller distinguishes create vs re-render
		}
		return nil, errorx.ExternalError.Wrap(err, "failed to read policy registry %s", registryPath(dir, name))
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse policy registry %s", registryPath(dir, name))
	}
	return &p, nil
}

// IsRegistryEmpty reports whether the policy registry at dir contains no
// entries. A missing directory is treated as empty. On error the bool is false
// so a failed read can never be mistaken for "empty" (len(nil)==0) and cause a
// caller to skip work it should not.
func IsRegistryEmpty(dir string) (bool, error) {
	policies, err := loadAll(dir)
	if err != nil {
		return false, err
	}
	return len(policies) == 0, nil
}

// loadAll reads every policy registry file in dir, sorted by name for a
// deterministic render. A missing directory yields an empty slice (the first
// create renders from a single in-memory entry before the dir exists).
func loadAll(dir string) ([]*Policy, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errorx.ExternalError.Wrap(err, "failed to read policy registry dir %s", dir)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(names)

	policies := make([]*Policy, 0, len(names))
	for _, n := range names {
		p, err := readEntry(dir, n)
		if err != nil {
			return nil, err
		}
		if p != nil {
			policies = append(policies, p)
		}
	}
	return policies, nil
}
