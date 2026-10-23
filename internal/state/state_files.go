// SPDX-License-Identifier: Apache-2.0

package state

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"

	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// legacyStateFiles is the pre-split state in dir, as glob patterns: state.yaml,
// the copies the per-component split leaves beside it, and the marker files the
// unified-state migration folds in. No command writes any of them.
func legacyStateFiles(dir string) []string {
	return []string{
		filepath.Join(dir, StateFileName),
		filepath.Join(dir, StateFileName+legacyStateBackupSuffix),
		filepath.Join(dir, StateFileName+".diverged-*"),
		filepath.Join(dir, "*.installed"),
		filepath.Join(dir, "*.configured"),
	}
}

// fileSnapshot maps each state file that exists to a hash of its content.
type fileSnapshot map[string]string

// snapshotFiles hashes every file matching patterns. A component file is
// hashed by its Hashable() content, the way Commit compares it, so a rewrite
// that only moves timestamps is not a change; any other file by its bytes.
func snapshotFiles(patterns []string) (fileSnapshot, error) {
	snap := fileSnapshot{}
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return nil, errorx.InternalError.Wrap(err, "invalid state file pattern %s", pattern)
		}
		for _, path := range paths {
			b, err := os.ReadFile(path)
			if err != nil {
				return nil, errorx.ExternalError.Wrap(err, "failed to read %s", path)
			}
			snap[path] = contentHash(path, b)
		}
	}
	return snap, nil
}

func contentHash(path string, b []byte) string {
	if _, ok := componentOfFile(path); ok {
		var s State
		if yaml.Unmarshal(b, &s) == nil {
			if h, err := stateContentHash(s); err == nil {
				return h
			}
		}
	}
	sum := sha256.Sum256(b)
	return "raw:" + hex.EncodeToString(sum[:])
}

// changedFiles lists the files created, changed or removed between before and
// after, as the action history names them: components first, in
// AllComponentIDs order, then provisioner, then legacy.
func changedFiles(before, after fileSnapshot) []ChangedFile {
	seen := map[ChangedFile]bool{}
	for path, h := range before {
		if after[path] != h {
			seen[fileOf(path)] = true
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			seen[fileOf(path)] = true
		}
	}

	order := make([]ChangedFile, 0, len(AllComponentIDs)+2)
	for _, id := range AllComponentIDs {
		order = append(order, id.File())
	}
	order = append(order, ChangedProvisioner, ChangedLegacy)

	var files []ChangedFile
	for _, f := range order {
		if seen[f] {
			files = append(files, f)
		}
	}
	return files
}

// componentOfFile returns the component whose file path is.
func componentOfFile(path string) (ComponentID, bool) {
	name := filepath.Base(path)
	i := slices.IndexFunc(AllComponentIDs, func(id ComponentID) bool { return string(id)+".yaml" == name })
	if i < 0 {
		return "", false
	}
	return AllComponentIDs[i], true
}

// fileOf names path the way the action history does.
func fileOf(path string) ChangedFile {
	if id, ok := componentOfFile(path); ok {
		return id.File()
	}
	if filepath.Base(path) == ProvisionerFileName {
		return ChangedProvisioner
	}
	return ChangedLegacy
}

// componentFiles is every component's file in dir.
func componentFiles(dir string) []string {
	paths := make([]string, 0, len(AllComponentIDs))
	for _, id := range AllComponentIDs {
		paths = append(paths, componentFilePath(dir, id))
	}
	return paths
}
