// SPDX-License-Identifier: Apache-2.0

// migrations.go provides state-layer helpers used by the migration framework.
//
// Individual migration implementations are in separate migration_*.go files:
//   - migration_unified_state.go:           Handles unified state file migration (legacy *.installed/*.configured → state.yaml)
//   - migration_helm_release_schema_v2.go:  Migrates HelmReleaseInfo schema from v1 to v2
//
// All migrations are registered centrally in cmd/weaver/commands/root.go RegisterMigrations().
// See docs/dev/migration-framework.md for the full guide.

package state

import (
	"os"
	"path/filepath"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// ReadProvisionerVersionFromDisk extracts the provisioner version from the on-disk state file
// without loading the full state into memory. Returns an empty string when no state file exists.
func ReadProvisionerVersionFromDisk() (string, error) {
	stateFile := filepath.Join(models.Paths().StateDir, StateFileName)

	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", errorx.InternalError.Wrap(err, "failed to read state file at %s", stateFile)
	}

	// Use a minimal struct that mirrors only the path we need:
	//   state:
	//     provisioner:
	//       version: "v0.x.y"
	var doc struct {
		State struct {
			Provisioner struct {
				Version string `yaml:"version"`
			} `yaml:"provisioner"`
		} `yaml:"state"`
	}

	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to parse state file at %s", stateFile)
	}

	return doc.State.Provisioner.Version, nil
}
