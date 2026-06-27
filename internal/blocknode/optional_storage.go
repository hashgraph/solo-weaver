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
//   4. Register the migration in cmd/cli/commands/root.go RegisterMigrations()
//
// Retiring an existing storage (a chart version removes a volume) is the mirror
// operation: set MaxVersion to the first chart version that no longer ships the
// volume. The registry filter (GetApplicableOptionalStorages) will drop the
// entry from that version onward; the old PV/PVC on already-installed clusters
// remains untouched (chart no longer references it; cleanup is a manual step).

package blocknode

import (
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/semver"
	"github.com/joomcode/errorx"
)

// BlockNodeApplicationStateRequiredVersion is the chart version at which the
// applicationStateFacility volume first appears (introduced by
// hiero-ledger/hiero-block-node#3025). solo-weaver creates the PV/PVC and
// fires the upgrade-time storage migration at this boundary.
//
// As of this PR, #3025 is on the upstream `main` branch but NOT cherry-picked
// to 0.36.x, so the volume first ships in 0.37.0. If upstream changes course
// and cherry-picks the volume into a 0.36.x release before 0.37.0 ships, bump
// this constant to that cherry-pick tag — no other structural change is needed.
//
// The "-0" suffix is the lowest-possible prerelease per semver §11, so the
// boundary is satisfied by every prerelease of 0.37.0 (0.37.0-rc1, -rc2, …) as
// well as the final 0.37.0 tag. Without it, semver ranks `0.37.0-rc1 < 0.37.0`
// and release candidates would be wrongly excluded from the cutover. This is a
// comparison bound only — user-facing text (flag help, docs) should say "0.37.0".
const BlockNodeApplicationStateRequiredVersion = "0.37.0-0"

// BlockNodeVerificationRetirementVersion is the chart version at which the
// dedicated verification volume is removed from the Helm chart (also
// hiero-ledger/hiero-block-node#3025). Kept as a separate constant from
// BlockNodeApplicationStateRequiredVersion so a cherry-pick scenario that
// introduces the new volume before retiring the old one can be expressed by
// bumping only one of the two.
//
// Carries the same "-0" prerelease floor as
// BlockNodeApplicationStateRequiredVersion so verification retires in lockstep
// across the 0.37.0 release candidates, not just the final tag.
const BlockNodeVerificationRetirementVersion = "0.37.0-0"

// OptionalStorage describes an optional storage volume that is conditionally
// required based on the target Block Node chart version.
type OptionalStorage struct {
	// Name is the kebab-case identifier used in logs, directory names, and CLI
	// flag names (e.g., "verification", "plugins", "application-state"). It is
	// NOT necessarily the Helm persistence key — see PersistenceKey, since some
	// chart keys are camelCase (application-state → applicationState).
	Name string

	// PersistenceKey is the key under blockNode.persistence in the chart values
	// that wires this volume's PVC. Set it only when the chart key differs from
	// Name (application-state's chart key is camelCase "applicationState");
	// callers fall back to Name when it is empty, which covers the common case
	// where the chart key matches the kebab-case name (verification, plugins).
	PersistenceKey string

	// MinVersion is the minimum Block Node version at which solo-weaver
	// provisions the PV/PVC. Drives `CreatePersistentVolumes` (install path)
	// and the registry filter used by `GetApplicableOptionalStorages` /
	// `RequiredByVersion`.
	MinVersion string

	// MaxVersion, when non-empty, is the exclusive upper bound: the storage is
	// required only while target < MaxVersion. Use this to retire a storage that
	// has been removed from the chart in a newer version. Empty means unbounded
	// above.
	MaxVersion string

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
		MaxVersion: BlockNodeVerificationRetirementVersion,
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
	{
		Name:           "application-state",
		PersistenceKey: "applicationState",
		MinVersion:     BlockNodeApplicationStateRequiredVersion,
		PVName:         "application-state-storage-pv",
		PVCName:        "application-state-storage-pvc",
		DirName:        "application-state",
		GetPath:        func(s *models.BlockNodeStorage) string { return s.ApplicationStatePath },
		SetPath:        func(s *models.BlockNodeStorage, p string) { s.ApplicationStatePath = p },
		GetSize:        func(s *models.BlockNodeStorage) string { return s.ApplicationStateSize },
	},
}

// GetOptionalStorages returns the full list of registered optional storages.
func GetOptionalStorages() []OptionalStorage {
	copied := make([]OptionalStorage, len(optionalStorages))
	copy(copied, optionalStorages)
	return copied
}

// RequiredByVersion returns true if the given target version requires this
// optional storage. The range is [MinVersion, MaxVersion) — MaxVersion empty
// means unbounded above.
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

	if target.LessThan(minVer) {
		return false
	}

	if o.MaxVersion == "" {
		return true
	}

	maxVer, err := semver.NewSemver(o.MaxVersion)
	if err != nil {
		// Programming error: constant is invalid; treat as unbounded.
		return true
	}

	return target.LessThan(maxVer)
}

// ValidateStorageCompleteness checks that enough storage paths are set to
// resolve all required paths for the given chart version. Either basePath must
// be set (to derive missing paths) or all required individual paths must be
// explicit. This includes core paths (archive, live, log) and version-dependent
// optional paths (verification, plugins, application-state).
func ValidateStorageCompleteness(storage models.BlockNodeStorage, chartVersion string) error {
	// If basePath is set, all missing paths can be derived.
	if strings.TrimSpace(storage.BasePath) != "" {
		return nil
	}
	// Without basePath, all core paths must be explicit.
	if strings.TrimSpace(storage.ArchivePath) == "" ||
		strings.TrimSpace(storage.LivePath) == "" ||
		strings.TrimSpace(storage.LogPath) == "" {
		return errorx.IllegalArgument.New(
			"at least one storage path is not set and base path is empty; set --base-path flag or storage.basePath in config")
	}
	// Without basePath, version-dependent optional paths must also be explicit.
	for _, opt := range GetApplicableOptionalStorages(chartVersion) {
		if strings.TrimSpace(opt.GetPath(&storage)) == "" {
			return errorx.IllegalArgument.New(
				"%s storage path is required for chart version %s; set --base-path (or storage.basePath in config) or --%s-path",
				opt.Name, chartVersion, opt.Name)
		}
	}
	return nil
}

// GetApplicableOptionalStorages returns the subset of optional storages that
// are required by the given target version. Use this for PV/PVC provisioning
// decisions (CreatePersistentVolumes, migration registration) AND helm-values
// rendering (ComputeValuesFile, injectPersistenceOverrides) — since the
// chart-mount and provisioning boundaries are the same.
func GetApplicableOptionalStorages(targetVersion string) []OptionalStorage {
	var applicable []OptionalStorage
	for _, os := range optionalStorages {
		if os.RequiredByVersion(targetVersion) {
			applicable = append(applicable, os)
		}
	}
	return applicable
}
