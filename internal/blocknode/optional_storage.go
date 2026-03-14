// SPDX-License-Identifier: Apache-2.0

// optional_storage.go provides a data-driven registry of optional storage types
// that are conditionally required based on the target Block Node version.
//
// Each new PV/PVC that the Block Node Helm chart introduces in a new version
// should be registered here as an OptionalStorage entry. The rest of the codebase
// (SetupStorage, CreatePersistentVolumes, GetStoragePaths, ComputeValuesFile, etc.)
// iterates over this registry, so adding a new storage type requires only:
//   1. Add an entry to optionalStorages below
//   2. Add the corresponding field(s) to models.BlockNodeStorage (if not already present)
//   3. Add the storage section to the Go-templated values YAML (using {{- if .Include<Name> }})
//   4. Register a migration in migrations.go via InitMigrations()

package blocknode

import (
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/semver"
)

// OptionalStorage describes an optional storage volume that is conditionally
// required based on the target Block Node chart version.
type OptionalStorage struct {
	// Name is the human-readable name used in logs and template field names
	// (e.g., "verification", "plugins"). Must match the Helm persistence key.
	Name string

	// MinVersion is the minimum Block Node version that requires this storage.
	MinVersion string

	// PVName is the PersistentVolume resource name (e.g., "verification-storage-pv").
	PVName string

	// PVCName is the PersistentVolumeClaim resource name (e.g., "verification-storage-pvc").
	PVCName string

	// DirName is the subdirectory name under basePath (e.g., "verification").
	DirName string

	// GetPath returns the models.red path from the storage models.
	GetPath func(s *models.BlockNodeStorage) string

	// SetPath sets the path in the storage models.
	SetPath func(s *models.BlockNodeStorage, p string)

	// GetSize returns the models.red size from the storage models.
	GetSize func(s *models.BlockNodeStorage) string
}

// optionalStorages is the canonical registry of all optional storage types.
// Entries must be in chronological order (by MinVersion).
var optionalStorages = []OptionalStorage{
	{
		Name:       "verification",
		MinVersion: "0.26.2",
		PVName:     "verification-storage-pv",
		PVCName:    "verification-storage-pvc",
		DirName:    "verification",
		GetPath:    func(s *models.BlockNodeStorage) string { return s.VerificationPath },
		SetPath:    func(s *models.BlockNodeStorage, p string) { s.VerificationPath = p },
		GetSize:    func(s *models.BlockNodeStorage) string { return s.VerificationSize },
	},
	{
		Name:       "plugins",
		MinVersion: "0.28.1",
		PVName:     "plugins-storage-pv",
		PVCName:    "plugins-storage-pvc",
		DirName:    "plugins",
		GetPath:    func(s *models.BlockNodeStorage) string { return s.PluginsPath },
		SetPath:    func(s *models.BlockNodeStorage, p string) { s.PluginsPath = p },
		GetSize:    func(s *models.BlockNodeStorage) string { return s.PluginsSize },
	},
}

// GetOptionalStorages returns the full list of registered optional storages.
func GetOptionalStorages() []OptionalStorage {
	copied := make([]OptionalStorage, len(optionalStorages))
	copy(copied, optionalStorages)
	return copied
}

// RequiredByVersion returns true if the given target version requires this optional storage.
func (o *OptionalStorage) RequiredByVersion(targetVersion string) bool {
	target, err := semver.NewSemver(targetVersion)
	if err != nil {
		return false
	}

	minVer, err := semver.NewSemver(o.MinVersion)
	if err != nil {
		// Programming error: constant is invalid
		return false
	}

	return !target.LessThan(minVer)
}

// GetApplicableOptionalStorages returns the subset of optional storages
// that are required by the given target version.
func GetApplicableOptionalStorages(targetVersion string) []OptionalStorage {
	var applicable []OptionalStorage
	for _, os := range optionalStorages {
		if os.RequiredByVersion(targetVersion) {
			applicable = append(applicable, os)
		}
	}
	return applicable
}
