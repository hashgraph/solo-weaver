// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/eventlog"
)

// HIP-defined JSONL event reasons — written verbatim to consensus-migrate-events.jsonl.
// Do NOT change without a corresponding HIP amendment; external tooling (Alloy
// pipelines, Loki alert rules) depends on their exact spelling.
const (
	ReasonSoakStarted           = "SoakStarted"
	ReasonSoakResumed           = "SoakResumed" // daemon restarted mid-soak; elapsed time preserved
	ReasonSoakCheck             = "SoakCheck"
	ReasonCriterionMet          = "CriterionMet"
	ReasonFleetThresholdReached = "FleetThresholdReached"
	ReasonDecommissionTriggered = "DecommissionTriggered"
	ReasonDecommissionCompleted = "DecommissionCompleted"
	ReasonDecommissionFailed    = "DecommissionFailed"
	ReasonSoakWatcherPanicked   = "SoakWatcherPanicked"  // panic recovered in watcher goroutine
	ReasonSoakStateCorrupted    = "SoakStateCorrupted"   // state file malformed/invalid; soak abandoned
	ReasonSoakStateWriteFailed  = "SoakStateWriteFailed" // state file not persisted; crash recovery compromised
)

// Journald-only log reasons — written to daemon stdout → journald for ops debugging.
// These are NOT written to the JSONL audit log; no external tooling depends on them.
const (
	reasonSoakDispatcherStopped = "SoakDispatcherStopped"
	reasonSoakPanic             = "SoakPanic"
	reasonSoakStopped           = "SoakStopped"
	reasonSoakWatcherStopped    = "SoakWatcherStopped"
	reasonSoakStateDeleteFailed = "SoakStateDeleteFailed"
	reasonSoakNoStateFile       = "SoakNoStateFile"
	reasonSoakStateReadFailed   = "SoakStateReadFailed"
	reasonSoakStateInvalid      = "SoakStateInvalid"
	reasonSoakResuming          = "SoakResuming"
)

// MigrationMonitorConfig holds tunable parameters for MigrationMonitor.
type MigrationMonitorConfig struct {
	// PollInterval is how often the soak-watcher evaluates criteria.
	// Zero value defaults to 900s (15 min) per HIP spec.
	PollInterval time.Duration

	// FleetThresholdPath is the host flag file whose presence means ≥26/39
	// fleet nodes have migrated successfully. Zero value defaults to the
	// HIP-specified path below.
	FleetThresholdPath string
}

// MigrationMonitor manages the migration soak lifecycle: it owns the activation
// channel, shared status, and the goroutine that monitors soak criteria.
// Once all mainnet nodes are migrated to the new deployment model, this can be safely disabled or removed from the
// codebase.
type MigrationMonitor struct {
	// soakStartCh carries activation requests from POST /migration/consensus/soak/start.
	// Buffered 1 so the HTTP handler never blocks.
	soakStartCh chan SoakStartRequest

	// soakStatus is the current watcher state. nil means idle.
	// atomic.Pointer[T] gives compile-time type safety with no mutex on reads.
	soakStatus atomic.Pointer[SoakStatusResponse]

	// soakActive is set to true before a watcher goroutine is spawned and
	// cleared when it exits. Checked by TryEnqueue to close the race window
	// between goroutine spawn and the first soakStatus store.
	soakActive atomic.Bool

	// soakWg tracks in-flight watcher goroutines so Run waits for full
	// quiescence before returning.
	soakWg sync.WaitGroup

	nodeID         string
	logger         *eventlog.EventLogger // nil-safe; logMigrateEvent checks before use
	decommissioner Decommissioner
	cfg            MigrationMonitorConfig
	stateFilePath  string // full path to cutover-state.jsonl
	criteria       []SoakCriterion
}

// NewMigrationMonitor returns a zero-config MigrationMonitor. Provided for
// backward compatibility with existing tests that only need the dispatch loop
// and HTTP handler wiring. Production code should use NewMigrationMonitorWith.
func NewMigrationMonitor() *MigrationMonitor {
	return newMigrationMonitor("", nil, &NoopDecommissioner{}, MigrationMonitorConfig{}, "")
}

// NewMigrationMonitorWith constructs a MigrationMonitor wired with the given dependencies.
func NewMigrationMonitorWith(nodeID string, logger *eventlog.EventLogger, d Decommissioner, cfg MigrationMonitorConfig, stateDir string) *MigrationMonitor {
	return newMigrationMonitor(nodeID, logger, d, cfg, stateDir)
}

// newMigrationMonitor is the shared internal constructor.
func newMigrationMonitor(nodeID string, logger *eventlog.EventLogger, d Decommissioner, cfg MigrationMonitorConfig, stateDir string) *MigrationMonitor {
	var stateFilePath string
	if stateDir != "" {
		stateFilePath = filepath.Join(stateDir, "cutover-state.jsonl")
	}
	return &MigrationMonitor{
		soakStartCh:    make(chan SoakStartRequest, 1),
		nodeID:         nodeID,
		logger:         logger,
		decommissioner: d,
		cfg:            cfg,
		stateFilePath:  stateFilePath,
	}
}

// WithCriteria sets the soak criteria to evaluate on each poll tick.
// Call before Run — not safe to call concurrently.
func (mm *MigrationMonitor) WithCriteria(criteria ...SoakCriterion) *MigrationMonitor {
	mm.criteria = criteria
	return mm
}

// pollInterval returns the configured poll interval, defaulting to 900s.
func (mm *MigrationMonitor) pollInterval() time.Duration {
	if mm.cfg.PollInterval > 0 {
		return mm.cfg.PollInterval
	}
	return 900 * time.Second
}

// fleetThresholdPath returns the configured fleet threshold flag file path,
// defaulting to the HIP-specified location.
func (mm *MigrationMonitor) fleetThresholdPath() string {
	if mm.cfg.FleetThresholdPath != "" {
		return mm.cfg.FleetThresholdPath
	}
	return "/opt/solo/weaver/migration/fleet-threshold-reached"
}

// logMigrateEvent is nil-safe: if the logger was not injected (e.g. directory
// creation failed at startup), events are silently dropped to journald only.
func (mm *MigrationMonitor) logMigrateEvent(e eventlog.Event) {
	if mm.logger == nil {
		return
	}
	if err := mm.logger.Log(e); err != nil {
		logx.As().Warn().Err(err).
			Str("reason", e.Reason).
			Msg("Failed to write migrate event to JSONL")
	}
}

// migrationOperationID returns a stable operation ID derived from the cutover
// timestamp — unique per migration and embeds the cutover time for auditability.
func migrationOperationID(req SoakStartRequest) string {
	return "migration-" + req.CutoverTimestamp.UTC().Format("20060102T150405Z")
}

// writeSoakState atomically writes req to path via a .tmp file + rename.
func writeSoakState(path string, req SoakStartRequest) error {
	b, err := json.Marshal(req)
	if err != nil {
		return ErrSoakWatcher.Wrap(err, "marshal soak state")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return ErrSoakWatcher.Wrap(err, "write soak state tmp file")
	}
	if err := os.Rename(tmp, path); err != nil {
		return ErrSoakWatcher.Wrap(err, "rename soak state file")
	}
	return nil
}

// allCriteriaGreen returns true only when every registered criterion returns
// (true, nil). On error, logs a warning and treats that criterion as not-green
// — a flaky check never triggers decommission.
func (mm *MigrationMonitor) allCriteriaGreen(ctx context.Context, req SoakStartRequest) bool {
	for _, c := range mm.criteria {
		ok, err := c.Check(ctx, req)
		if err != nil {
			logx.As().Warn().Err(err).
				Str("criterion", c.Name()).
				Msg("Soak criterion check error — treating as not-green")
			return false
		}
		if !ok {
			return false
		}
	}
	return true
}

// TryEnqueue queues a soak activation request. Returns false if the request
// cannot be accepted; the caller should respond with 409 Conflict. Two
// conditions both map to false:
//   - a watcher goroutine is already running (soakActive = true)
//   - the channel is already full (a prior request is queued but not yet dispatched)
//
// Both cases are intentionally indistinguishable to callers — only one soak
// activation may be in-flight at any time.
//
// soakStatus is set to Active=true here, synchronously, so that
// GET /soak/status reflects the accepted request immediately — without waiting
// for the watcher goroutine (spawned by Run) to store the same value. The
// watcher stores it again on startup (idempotent), and clears it on exit.
func (mm *MigrationMonitor) TryEnqueue(req SoakStartRequest) bool {
	if mm.soakActive.Swap(true) {
		return false
	}
	select {
	case mm.soakStartCh <- req:
		reqCopy := req
		mm.soakStatus.Store(&SoakStatusResponse{Active: true, Request: &reqCopy})
		return true
	default:
		mm.soakActive.Store(false)
		return false
	}
}

// idleSoakStatus is the sentinel returned by Status when no watcher is active.
var idleSoakStatus = &SoakStatusResponse{Active: false}

// Status returns the current soak status. Never returns nil.
// Returns the live pointer when a watcher is active to avoid a copy on the
// hot GET /migration/consensus/soak/status read path.
func (mm *MigrationMonitor) Status() *SoakStatusResponse {
	if p := mm.soakStatus.Load(); p != nil {
		return p
	}
	return idleSoakStatus
}

// Name implements daemon.MonitorRunner.
func (mm *MigrationMonitor) Name() string { return "migration-monitor" }

// Run is the dispatch loop. It blocks until ctx is cancelled, then waits for
// any in-flight watcher goroutines to finish before returning.
func (mm *MigrationMonitor) Run(ctx context.Context) error {
	defer mm.soakWg.Wait()

	mm.resumeIfNeeded(ctx)

	for {
		select {
		case req := <-mm.soakStartCh:
			// soakWg.Add before goroutine start so the deferred soakWg.Wait()
			// above accounts for it.
			mm.soakWg.Add(1)
			go mm.run(ctx, req)
		case <-ctx.Done():
			// Intentional: if soakStartCh has a buffered item at the same time
			// ctx is cancelled, Go's select may pick either case. The spawned
			// watcher exits immediately on ctx.Done() and soakWg stays balanced.
			logx.As().Info().Str("reason", reasonSoakDispatcherStopped).Msg("Soak dispatcher stopped")
			return nil
		}
	}
}

// run is the per-activation watcher goroutine. It is not inside the errgroup
// so a watcher failure does not cancel the whole daemon.
func (mm *MigrationMonitor) run(ctx context.Context, req SoakStartRequest) {
	// Single outermost defer: recovery wraps all cleanup so a panic in the
	// cleanup path is caught rather than silently replacing the original panic.
	defer func() {
		if r := recover(); r != nil {
			logx.As().Error().Str("reason", reasonSoakPanic).
				Str("node_id", req.NodeID).
				Str("panic", fmt.Sprintf("%v", r)).
				Msg("Soak watcher panicked")
			mm.logMigrateEvent(eventlog.Event{
				Ts:          time.Now().UTC(),
				Level:       eventlog.LevelError,
				Reason:      ReasonSoakWatcherPanicked,
				Msg:         fmt.Sprintf("Soak watcher panicked: %v", r),
				OperationID: migrationOperationID(req),
				NodeID:      req.NodeID,
			})
		}
		mm.soakStatus.Store(nil)
		mm.soakActive.Store(false)
		logx.As().Info().Str("reason", reasonSoakStopped).Str("node_id", req.NodeID).Msg("Soak watcher stopped")
		mm.soakWg.Done()
	}()

	opID := migrationOperationID(req)

	// Emit SoakStarted
	mm.logMigrateEvent(eventlog.Event{
		Ts:          time.Now().UTC(),
		Level:       eventlog.LevelInfo,
		Reason:      ReasonSoakStarted,
		Msg:         fmt.Sprintf("Soak started for node %s; cutover at %s", req.NodeID, req.CutoverTimestamp.UTC().Format(time.RFC3339)),
		OperationID: opID,
		NodeID:      req.NodeID,
	})

	mm.soakStatus.Store(&SoakStatusResponse{Active: true, Request: &req})

	// Persist state for crash recovery — NOT deleted on clean shutdown, only on decommission.
	if mm.stateFilePath != "" {
		if err := writeSoakState(mm.stateFilePath, req); err != nil {
			logx.As().Warn().Err(err).Str("reason", ReasonSoakStateWriteFailed).Msg("Failed to write soak state file — crash recovery will not be possible")
			mm.logMigrateEvent(eventlog.Event{
				Ts:          time.Now().UTC(),
				Level:       eventlog.LevelError,
				Reason:      ReasonSoakStateWriteFailed,
				Msg:         fmt.Sprintf("Failed to persist soak state: %v — crash recovery will not be possible", err),
				OperationID: opID,
				NodeID:      req.NodeID,
			})
		}
	}

	ticker := time.NewTicker(mm.pollInterval())
	defer ticker.Stop()

	// greenCriteria tracks which criteria have already had CriterionMet emitted
	// so we emit it only once per criterion (on the false→true transition).
	greenCriteria := make(map[string]bool)
	fleetThresholdEmitted := false

	for {
		select {
		case <-ctx.Done():
			// State file is intentionally NOT deleted — daemon restart will resume
			// from the persisted cutover_timestamp without losing elapsed soak time.
			logx.As().Info().Str("reason", reasonSoakWatcherStopped).Str("node_id", req.NodeID).
				Msg("Soak watcher stopping due to context cancellation — state preserved for resume")
			return

		case <-ticker.C:
			now := time.Now().UTC()
			soakHours := time.Since(req.CutoverTimestamp).Hours()
			pollSecs := int(mm.pollInterval().Seconds())

			// Evaluate each criterion; emit CriterionMet on first green transition.
			uploaderCleared := false
			podRestarts := 0
			for _, c := range mm.criteria {
				ok, err := c.Check(ctx, req)
				if err != nil {
					logx.As().Warn().Err(err).Str("criterion", c.Name()).Msg("Soak criterion check error — treating as not-green")
					continue
				}
				// Capture per-criterion values for SoakCheck payload.
				switch c.Name() {
				case "UploaderBacklogCleared":
					uploaderCleared = ok
				}
				// Populate restart count from any criterion that tracks it.
				if rc, ok2 := c.(RestartCounter); ok2 {
					podRestarts += rc.TotalRestarts()
				}
				if ok && !greenCriteria[c.Name()] {
					greenCriteria[c.Name()] = true
					mm.logMigrateEvent(eventlog.Event{
						Ts:          now,
						Level:       eventlog.LevelInfo,
						Reason:      ReasonCriterionMet,
						Msg:         fmt.Sprintf("Soak criterion met: %s", c.Name()),
						OperationID: opID,
						NodeID:      req.NodeID,
					})
				}
			}

			// Check fleet threshold flag file; emit FleetThresholdReached once.
			fleetNodesMigrated := 0
			if _, err := os.Stat(mm.fleetThresholdPath()); err == nil {
				fleetNodesMigrated = 1 // flag file present; exact count via REST API in a future story
				if !fleetThresholdEmitted {
					fleetThresholdEmitted = true
					mm.logMigrateEvent(eventlog.Event{
						Ts:          now,
						Level:       eventlog.LevelInfo,
						Reason:      ReasonFleetThresholdReached,
						Msg:         "Fleet migration threshold reached — flag file present at " + mm.fleetThresholdPath(),
						OperationID: opID,
						NodeID:      req.NodeID,
					})
				}
			}

			// Emit SoakCheck unconditionally every tick — dual purpose:
			// (1) progress snapshot; (2) goroutine liveness heartbeat.
			// Suppressing this event is a bug — its absence is the failure signal.
			mm.logMigrateEvent(eventlog.Event{
				Ts:     now,
				Level:  eventlog.LevelInfo,
				Reason: ReasonSoakCheck,
				Msg: fmt.Sprintf(
					"Soak check: %.2fh elapsed, uploader_backlog_cleared=%v, pod_restarts_since_cutover=%d, fleet_nodes_migrated=%d, next_check_in_seconds=%d",
					soakHours, uploaderCleared, podRestarts, fleetNodesMigrated, pollSecs,
				),
				OperationID: opID,
				NodeID:      req.NodeID,
			})

			// Decommission gate: all criteria green AND fleet threshold reached.
			if fleetThresholdEmitted && mm.allCriteriaGreen(ctx, req) {
				mm.logMigrateEvent(eventlog.Event{
					Ts:          time.Now().UTC(),
					Level:       eventlog.LevelInfo,
					Reason:      ReasonDecommissionTriggered,
					Msg:         fmt.Sprintf("All soak criteria met and fleet threshold reached — triggering decommission for node %s", req.NodeID),
					OperationID: opID,
					NodeID:      req.NodeID,
				})

				if err := mm.decommissioner.Decommission(ctx, req.NodeID); err != nil {
					logx.As().Error().Err(err).Str("reason", ReasonDecommissionFailed).Str("node_id", req.NodeID).Msg("Decommission failed")
					mm.logMigrateEvent(eventlog.Event{
						Ts:          time.Now().UTC(),
						Level:       eventlog.LevelError,
						Reason:      ReasonDecommissionFailed,
						Msg:         fmt.Sprintf("Decommission failed: %v", err),
						OperationID: opID,
						NodeID:      req.NodeID,
					})
					return
				}

				mm.logMigrateEvent(eventlog.Event{
					Ts:          time.Now().UTC(),
					Level:       eventlog.LevelInfo,
					Reason:      ReasonDecommissionCompleted,
					Msg:         fmt.Sprintf("Decommission completed for node %s", req.NodeID),
					OperationID: opID,
					NodeID:      req.NodeID,
				})

				// Clean exit: delete state file so a subsequent daemon start knows no soak is pending.
				if mm.stateFilePath != "" {
					if err := os.Remove(mm.stateFilePath); err != nil && !os.IsNotExist(err) {
						logx.As().Warn().Err(err).Str("reason", reasonSoakStateDeleteFailed).Msg("Failed to delete soak state file after decommission")
					}
				}
				return
			}
		}
	}
}

// resumeIfNeeded reads cutover-state.jsonl on startup and re-activates the
// watcher if a migration soak was in progress before a daemon restart.
//
// Invariant: before spawning run(), this function:
//  1. calls mm.soakActive.Store(true) — keeps the duplicate-watcher guard consistent
//  2. calls mm.soakWg.Add(1) before the goroutine starts — keeps soakWg.Wait()
//     quiescence guarantee intact
func (mm *MigrationMonitor) resumeIfNeeded(ctx context.Context) {
	if mm.stateFilePath == "" {
		return
	}
	b, err := os.ReadFile(mm.stateFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			logx.As().Debug().Str("reason", reasonSoakNoStateFile).Msg("No soak state file — nothing to resume")
			return
		}
		logx.As().Warn().Err(err).Str("reason", reasonSoakStateReadFailed).Msg("Failed to read soak state file — skipping resume")
		return
	}

	var req SoakStartRequest
	if err := json.Unmarshal(b, &req); err != nil {
		logx.As().Warn().Err(err).Str("reason", reasonSoakStateInvalid).Msg("Soak state file is malformed — deleting and skipping resume")
		mm.logMigrateEvent(eventlog.Event{
			Ts:          time.Now().UTC(),
			Level:       eventlog.LevelError,
			Reason:      ReasonSoakStateCorrupted,
			Msg:         fmt.Sprintf("Soak state file is malformed — soak abandoned: %v", err),
			OperationID: "migration-unknown",
			NodeID:      mm.nodeID,
		})
		_ = os.Remove(mm.stateFilePath)
		return
	}
	if err := req.Validate(); err != nil {
		logx.As().Warn().Err(err).Str("reason", reasonSoakStateInvalid).Msg("Soak state file failed validation — deleting and skipping resume")
		mm.logMigrateEvent(eventlog.Event{
			Ts:          time.Now().UTC(),
			Level:       eventlog.LevelError,
			Reason:      ReasonSoakStateCorrupted,
			Msg:         fmt.Sprintf("Soak state file failed validation — soak abandoned: %v", err),
			OperationID: "migration-unknown",
			NodeID:      mm.nodeID,
		})
		_ = os.Remove(mm.stateFilePath)
		return
	}

	logx.As().Info().
		Str("reason", reasonSoakResuming).
		Str("node_id", req.NodeID).
		Time("cutover_ts", req.CutoverTimestamp).
		Float64("elapsed_hours", time.Since(req.CutoverTimestamp).Hours()).
		Msg("Resuming soak watcher from persisted state — elapsed time is preserved")

	mm.logMigrateEvent(eventlog.Event{
		Ts:          time.Now().UTC(),
		Level:       eventlog.LevelInfo,
		Reason:      ReasonSoakResumed,
		Msg:         fmt.Sprintf("Soak resumed after daemon restart for node %s; cutover at %s; %.2fh elapsed", req.NodeID, req.CutoverTimestamp.UTC().Format(time.RFC3339), time.Since(req.CutoverTimestamp).Hours()),
		OperationID: migrationOperationID(req),
		NodeID:      req.NodeID,
	})

	mm.soakActive.Store(true)
	mm.soakWg.Add(1)
	go mm.run(ctx, req)
}
