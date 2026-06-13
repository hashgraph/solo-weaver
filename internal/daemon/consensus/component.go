// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/daemonkit"
	"github.com/hashgraph/solo-weaver/pkg/eventlog"
)

// ComponentConfig holds all inputs needed to build the consensus-node component.
// Constructed by daemon.go from ConsensusNodeComponentConfig and WeaverPaths so
// that this package does not import the daemon package (avoiding cycles).
type ComponentConfig struct {
	NodeID           string
	KubeconfigPath   string
	Orbit            string
	UpgradeEnabled   bool
	MigrationEnabled bool
	UpgradeEventsDir string
	HomeDir          string
	UpgradeDir       string
	MigrateEventsDir string
}

// ComponentResult contains the monitors built by NewComponent and a reference
// to the MigrationMonitor (when enabled) so daemon.go can wire the HTTP handler
// with the per-component StatusTracker closure after the component is assembled.
type ComponentResult struct {
	// Monitors is the ordered slice of monitors to run under the supervisor.
	Monitors []daemonkit.MonitorRunner

	// MigrationMonitor is non-nil when the migration monitor is enabled.
	// daemon.go uses this to construct ConsensusNodeHandler with the correct
	// migrationStateFn closure after the component's StatusTracker is created.
	MigrationMonitor *MigrationMonitor
}

// NewComponent constructs all enabled monitors for the consensus-node component
// and returns them alongside any references needed for HTTP handler wiring.
// It is the single entry point for consensus-node component assembly — daemon.go
// only needs to call this and wire the result into the Daemon struct.
func NewComponent(cfg ComponentConfig) (ComponentResult, error) {
	var monitors []daemonkit.MonitorRunner

	if cfg.UpgradeEnabled {
		um, err := NewUpgradeMonitor(UpgradeMonitorConfig{
			NodeID:           cfg.NodeID,
			KubeconfigPath:   cfg.KubeconfigPath,
			Namespace:        cfg.Orbit,
			UpgradeEventsDir: cfg.UpgradeEventsDir,
			HomeDir:          cfg.HomeDir,
			UpgradeDir:       cfg.UpgradeDir,
		})
		if err != nil {
			return ComponentResult{}, err
		}
		monitors = append(monitors, um)
	}

	var mm *MigrationMonitor
	if cfg.MigrationEnabled {
		var migrateLogger *eventlog.EventLogger
		if ml, err := eventlog.NewAppend(cfg.MigrateEventsDir, "consensus-migrate-events.jsonl"); err != nil {
			logx.As().Warn().Err(err).
				Str("reason", "MigrateLoggerInitFailed").
				Str("dir", cfg.MigrateEventsDir).
				Msg("Failed to open migrate event logger — migration events will not be persisted")
		} else {
			migrateLogger = ml
		}

		mm = NewMigrationMonitorWith(
			cfg.NodeID,
			migrateLogger,
			&NoopDecommissioner{},
			MigrationMonitorConfig{},
			cfg.MigrateEventsDir,
		).WithCriteria(
			SoakDuration{}, // zero Period → defaults to DefaultSoakPeriod (48h)
			UploaderBacklogCleared{},
			&NoPodRestarts{
				KubeconfigPath: cfg.KubeconfigPath,
				Namespace:      cfg.Orbit,
				PodLabelSelector: fmt.Sprintf(
					"operator.solo.hedera.com/orbit=%s,operator.solo.hedera.com/node-id=%s",
					cfg.Orbit, cfg.NodeID,
				),
			},
			ConsensusParticipationNominal{},
		)
		monitors = append(monitors, mm)
	}

	return ComponentResult{
		Monitors:         monitors,
		MigrationMonitor: mm,
	}, nil
}
