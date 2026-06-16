// SPDX-License-Identifier: Apache-2.0

// Package selfupgrade defines the on-disk contracts for the daemon's binary
// self-upgrade protocol (epic #500): the self-upgrade.yaml state file and the
// .bak archived-binary naming convention.
//
// self-upgrade.yaml is written by the OLD daemon immediately before it spawns
// the detached upgrade process — at step 0, before any binary swap begins. It
// is both a handoff record and a crash-diagnostic artifact:
//
//   - Crash diagnostics: if the detached upgrader dies mid-swap, the recover
//     tool reads this file to learn what version was being installed, which step
//     it had reached, and whether the .bak binaries are still present.
//   - PID tracking: ChildPID lets an operator/recovery tool check whether the
//     detached process is still alive (kill -0).
//   - Recovery input: `solo-provisioner consensus node upgrade-recover` (#717)
//     inspects this file to decide whether to restore a .bak binary and restart
//     the daemon.
//
// Because the file is written before the swap, a leftover Status of "in-progress"
// is itself the failure signal: a clean run always ends with "succeeded". On
// failure the detached process writes "failed" and leaves the .bak files intact.
package selfupgrade

import (
	"os"
	"path/filepath"
	"time"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/schema"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// CurrentSchemaVersion is the schemaVersion written by this build. Increment it
// only when a breaking structural change is made to SelfUpgradeYAML, and add a
// corresponding sealed selfUpgradeV{N} struct + migration step.
const CurrentSchemaVersion = 1

var (
	// ErrNamespace groups self-upgrade state errors.
	ErrNamespace = errorx.NewNamespace("selfupgrade")

	// ErrState is returned when self-upgrade.yaml cannot be read or written.
	ErrState = ErrNamespace.NewType("state")
)

// Status is the lifecycle status recorded in self-upgrade.yaml.
type Status string

const (
	// StatusInProgress is written at step 0, before the binary swap begins. If it
	// survives to recovery time, the upgrade crashed mid-swap.
	StatusInProgress Status = "in-progress"

	// StatusSucceeded is written by the detached upgrader after a clean swap; the
	// .bak files are removed on success.
	StatusSucceeded Status = "succeeded"

	// StatusFailed is written by the detached upgrader on failure; the .bak files
	// are left intact for recovery.
	StatusFailed Status = "failed"
)

// SelfUpgradeYAML is the current in-memory shape of self-upgrade.yaml. It lives
// at WeaverPaths.SelfUpgradeYAMLPath (/opt/solo/weaver/daemon/self-upgrade.yaml).
type SelfUpgradeYAML struct {
	// SchemaVersion identifies the file format. Stamped to CurrentSchemaVersion by Save.
	SchemaVersion int `yaml:"schemaVersion"`

	// Timestamp is when the operation was initiated (RFC3339, UTC).
	Timestamp time.Time `yaml:"timestamp"`

	// OperationID ties this self-upgrade to the originating NetworkUpgradeExecute
	// CR's spec.operationId. Also embedded in the .bak filenames (see bak.go).
	OperationID string `yaml:"operationId"`

	// Status is the lifecycle marker — the primary success/failure signal.
	Status Status `yaml:"status"`

	// ChildPID is the PID of the detached upgrade process, for liveness checks.
	ChildPID int `yaml:"childPid,omitempty"`

	// CurrentStep is the last step the detached upgrader began. Used to localise
	// a crash during recovery diagnostics.
	CurrentStep string `yaml:"currentStep,omitempty"`

	// Version transition — the from/to versions of each binary being swapped.
	FromCLIVersion    string `yaml:"fromCliVersion,omitempty"`
	ToCLIVersion      string `yaml:"toCliVersion,omitempty"`
	FromDaemonVersion string `yaml:"fromDaemonVersion,omitempty"`
	ToDaemonVersion   string `yaml:"toDaemonVersion,omitempty"`

	// .bak references — absolute paths to the archived binaries the recover tool
	// restores when an upgrade is rolled back. Empty when no archive was taken yet.
	CLIBakPath    string `yaml:"cliBakPath,omitempty"`
	DaemonBakPath string `yaml:"daemonBakPath,omitempty"`
}

// selfUpgradeV1 is the sealed on-disk representation for schemaVersion: 1.
// Never modify it after it ships — introduce selfUpgradeV2 and a migration step
// instead. Field layout must match the YAML written by Save when v1 was current.
type selfUpgradeV1 struct {
	SchemaVersion     int       `yaml:"schemaVersion"`
	Timestamp         time.Time `yaml:"timestamp"`
	OperationID       string    `yaml:"operationId"`
	Status            Status    `yaml:"status"`
	ChildPID          int       `yaml:"childPid,omitempty"`
	CurrentStep       string    `yaml:"currentStep,omitempty"`
	FromCLIVersion    string    `yaml:"fromCliVersion,omitempty"`
	ToCLIVersion      string    `yaml:"toCliVersion,omitempty"`
	FromDaemonVersion string    `yaml:"fromDaemonVersion,omitempty"`
	ToDaemonVersion   string    `yaml:"toDaemonVersion,omitempty"`
	CLIBakPath        string    `yaml:"cliBakPath,omitempty"`
	DaemonBakPath     string    `yaml:"daemonBakPath,omitempty"`
}

// MigrateToLatest is the terminal step of the migration chain at v1.
func (v *selfUpgradeV1) MigrateToLatest() SelfUpgradeYAML {
	return SelfUpgradeYAML{
		SchemaVersion:     CurrentSchemaVersion,
		Timestamp:         v.Timestamp,
		OperationID:       v.OperationID,
		Status:            v.Status,
		ChildPID:          v.ChildPID,
		CurrentStep:       v.CurrentStep,
		FromCLIVersion:    v.FromCLIVersion,
		ToCLIVersion:      v.ToCLIVersion,
		FromDaemonVersion: v.FromDaemonVersion,
		ToDaemonVersion:   v.ToDaemonVersion,
		CLIBakPath:        v.CLIBakPath,
		DaemonBakPath:     v.DaemonBakPath,
	}
}

// stateSchema is the versioned loader for self-upgrade.yaml.
var stateSchema = schema.Versioned[SelfUpgradeYAML]{
	CurrentVersion: CurrentSchemaVersion,
	Factories: map[int]func() schema.Migratable[SelfUpgradeYAML]{
		1: func() schema.Migratable[SelfUpgradeYAML] { return &selfUpgradeV1{} },
	},
}

// DefaultPath returns the HIP-authoritative self-upgrade.yaml path for the
// default weaver home (/opt/solo/weaver/daemon/self-upgrade.yaml).
func DefaultPath() string {
	return models.Paths().SelfUpgradeYAMLPath
}

// Load reads and strict-decodes self-upgrade.yaml at path, rejecting unknown
// fields and any schemaVersion the running build does not support.
func Load(path string) (SelfUpgradeYAML, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SelfUpgradeYAML{}, ErrState.Wrap(err, "cannot read self-upgrade state at %s", path)
	}
	s, err := stateSchema.Decode(data)
	if err != nil {
		return SelfUpgradeYAML{}, err
	}
	return s, nil
}

// Save stamps SchemaVersion = CurrentSchemaVersion and atomically writes s to
// path (write-temp-then-rename so a crash mid-write never leaves a torn file),
// creating any missing parent directories.
func Save(path string, s SelfUpgradeYAML) error {
	s.SchemaVersion = CurrentSchemaVersion

	data, err := yaml.Marshal(s)
	if err != nil {
		return ErrState.Wrap(err, "cannot serialise self-upgrade state")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, models.DefaultDirOrExecPerm); err != nil {
		return ErrState.Wrap(err, "cannot create self-upgrade state directory %s", dir)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, models.DefaultFilePerm); err != nil {
		return ErrState.Wrap(err, "cannot write self-upgrade state to %s", tmp)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Best-effort cleanup so a failed finalise does not leave a stale .tmp
		// behind to accumulate or confuse recovery tooling.
		_ = os.Remove(tmp)
		return ErrState.Wrap(err, "cannot finalise self-upgrade state at %s", path)
	}
	return nil
}
