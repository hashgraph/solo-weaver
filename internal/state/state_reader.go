// SPDX-License-Identifier: Apache-2.0

// state_reader.go provides lightweight, read-only helpers that extract
// individual fields from the on-disk state file without loading the
// full State struct into memory.  They are used by the prompt layer
// (which runs before the runtime/state-manager exist) and by the
// migration framework (which needs the provisioner version early).

package state

import (
	"os"
	"path/filepath"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

const errParseStateFile = "failed to parse state file"

// unmarshalStateDoc is a test-visible helper that wraps yaml.Unmarshal with
// the standard error wrapping used by all reader functions.  Tests call it
// directly to verify YAML path mapping without touching the filesystem.
func unmarshalStateDoc(data []byte, doc interface{}) error {
	if err := yaml.Unmarshal(data, doc); err != nil {
		return errorx.InternalError.Wrap(err, errParseStateFile)
	}
	return nil
}

// readStateFileBytes returns the raw bytes of the on-disk state file.
// If the file does not exist it returns (nil, nil) so callers can
// treat a missing file as "empty state" without error handling.
func readStateFileBytes() ([]byte, error) {
	stateFile := filepath.Join(models.Paths().StateDir, StateFileName)

	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errorx.InternalError.Wrap(err, "failed to read state file at %s", stateFile)
	}
	return data, nil
}

// ── Doc struct types ─────────────────────────────────────────────────────────
// Named types used for partial YAML unmarshalling.  They are exported so that
// tests can reuse the exact same struct definitions and YAML tags, preventing
// drift between production code and test assertions.

// ProvisionerVersionDoc is the minimal YAML shape for extracting the provisioner version.
type ProvisionerVersionDoc struct {
	State struct {
		Provisioner struct {
			Version string `yaml:"version"`
		} `yaml:"provisioner"`
	} `yaml:"state"`
}

// PromptDefaultsDoc is the minimal YAML shape for extracting all prompt-relevant
// fields in a single parse: profile (from machineState) and block node fields
// (from blockNodeState, where HelmReleaseInfo is yaml:",inline").
type PromptDefaultsDoc struct {
	State struct {
		MachineState struct {
			Profile string `yaml:"profile"`
		} `yaml:"machineState"`
		BlockNodeState struct {
			Name              string `yaml:"name"`
			Namespace         string `yaml:"namespace"`
			ChartVersion      string `yaml:"version"`
			HistoricRetention string `yaml:"historicRetention"`
			RecentRetention   string `yaml:"recentRetention"`
			Storage           struct {
				BasePath         string `yaml:"basePath"`
				ArchivePath      string `yaml:"archivePath"`
				LivePath         string `yaml:"livePath"`
				LogPath          string `yaml:"logPath"`
				VerificationPath string `yaml:"verificationPath"`
				PluginsPath      string `yaml:"pluginsPath"`
			} `yaml:"storage"`
		} `yaml:"blockNodeState"`
	} `yaml:"state"`
}

// ReadProvisionerVersionFromDisk extracts the provisioner version from the on-disk state file
// without loading the full state into memory. Returns an empty string when no state file exists.
func ReadProvisionerVersionFromDisk() (string, error) {
	data, err := readStateFileBytes()
	if err != nil || data == nil {
		return "", err
	}

	var doc ProvisionerVersionDoc
	if err := unmarshalStateDoc(data, &doc); err != nil {
		return "", err
	}

	return doc.State.Provisioner.Version, nil
}

// BlockNodeSummary holds the prompt-relevant fields from BlockNodeState
// read from the on-disk state file.  HelmReleaseInfo is yaml:",inline"
// inside BlockNodeState, so its keys (name, namespace, version) live
// directly under blockNodeState in the YAML.
type BlockNodeSummary struct {
	ReleaseName       string
	Namespace         string
	ChartVersion      string
	HistoricRetention string
	RecentRetention   string
	Storage           models.BlockNodeStorage
}

// PromptDefaults holds all prompt-relevant fields extracted from the on-disk
// state file in a single read + parse pass.
type PromptDefaults struct {
	Profile   string
	BlockNode BlockNodeSummary
}

// ReadPromptDefaultsFromDisk extracts all prompt-relevant fields from the
// on-disk state file in a single read + YAML parse.  This avoids the overhead
// of reading and parsing the same file twice when both BlockNodeSelectPrompts
// and BlockNodeInputPrompts run in the same prompt flow.
// Returns a zero-value struct when no state file exists.
func ReadPromptDefaultsFromDisk() (PromptDefaults, error) {
	data, err := readStateFileBytes()
	if err != nil || data == nil {
		return PromptDefaults{}, err
	}

	var doc PromptDefaultsDoc
	if err := unmarshalStateDoc(data, &doc); err != nil {
		return PromptDefaults{}, err
	}

	bn := doc.State.BlockNodeState
	return PromptDefaults{
		Profile: doc.State.MachineState.Profile,
		BlockNode: BlockNodeSummary{
			ReleaseName:       bn.Name,
			Namespace:         bn.Namespace,
			ChartVersion:      bn.ChartVersion,
			HistoricRetention: bn.HistoricRetention,
			RecentRetention:   bn.RecentRetention,
			Storage: models.BlockNodeStorage{
				BasePath:         bn.Storage.BasePath,
				ArchivePath:      bn.Storage.ArchivePath,
				LivePath:         bn.Storage.LivePath,
				LogPath:          bn.Storage.LogPath,
				VerificationPath: bn.Storage.VerificationPath,
				PluginsPath:      bn.Storage.PluginsPath,
			},
		},
	}, nil
}
