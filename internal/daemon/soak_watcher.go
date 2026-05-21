// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/automa-saga/logx"
)

// SoakWatcher manages the migration soak lifecycle: it owns the activation
// channel, shared status, and the goroutine that monitors soak criteria.
// Stub — full implementation in story #520.
// Once all mainnet nodes are migrated to the new deployment model, this can be safely disabled or removed from the
// codebase.
type SoakWatcher struct {
	// soakStartCh carries activation requests from POST /soak/start.
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
}

func NewSoakWatcher() *SoakWatcher {
	return &SoakWatcher{
		soakStartCh: make(chan SoakStartRequest, 1),
	}
}

// TryEnqueue queues a soak activation request. Returns false if the request
// cannot be accepted; the caller should respond with 409 Conflict. Two
// conditions both map to false:
//   - a watcher goroutine is already running (soakActive = true)
//   - the channel is already full (a prior request is queued but not yet dispatched)
//
// Both cases are intentionally indistinguishable to callers — only one soak
// activation may be in-flight at any time.
func (sw *SoakWatcher) TryEnqueue(req SoakStartRequest) bool {
	if sw.soakActive.Swap(true) {
		return false
	}
	select {
	case sw.soakStartCh <- req:
		return true
	default:
		sw.soakActive.Store(false)
		return false
	}
}

// idleSoakStatus is the sentinel returned by Status when no watcher is active.
var idleSoakStatus = &SoakStatusResponse{Active: false}

// Status returns the current soak status. Never returns nil.
// Returns the live pointer when a watcher is active to avoid a copy on the
// hot GET /soak/status read path.
func (sw *SoakWatcher) Status() *SoakStatusResponse {
	if p := sw.soakStatus.Load(); p != nil {
		return p
	}
	return idleSoakStatus
}

// Run is the dispatch loop. It blocks until ctx is cancelled, then waits for
// any in-flight watcher goroutines to finish before returning.
func (sw *SoakWatcher) Run(ctx context.Context) error {
	defer sw.soakWg.Wait()

	sw.resumeIfNeeded(ctx)

	for {
		select {
		case req := <-sw.soakStartCh:
			// soakWg.Add before goroutine start so the deferred soakWg.Wait()
			// above accounts for it.
			sw.soakWg.Add(1)
			go sw.run(ctx, req)
		case <-ctx.Done():
			// Intentional: if soakStartCh has a buffered item at the same time
			// ctx is cancelled, Go's select may pick either case. The spawned
			// watcher exits immediately on ctx.Done() and soakWg stays balanced.
			logx.As().Info().Str("reason", "SoakDispatcherStopped").Msg("Soak dispatcher stopped")
			return nil
		}
	}
}

// run is the per-activation watcher goroutine. It is not inside the errgroup
// so a watcher failure does not cancel the whole daemon.
// Stub — implemented in story #520.
func (sw *SoakWatcher) run(ctx context.Context, req SoakStartRequest) {
	// Single outermost defer: recovery wraps all cleanup so a panic in the
	// cleanup path is caught rather than silently replacing the original panic.
	defer func() {
		if r := recover(); r != nil {
			logx.As().Error().Str("reason", "SoakPanic").
				Str("node_id", req.NodeID).
				Str("panic", fmt.Sprintf("%v", r)).
				Msg("Soak watcher panicked")
		}
		sw.soakStatus.Store(nil)
		sw.soakActive.Store(false)
		logx.As().Info().Str("reason", "SoakStopped").Str("node_id", req.NodeID).Msg("Soak watcher stopped")
		sw.soakWg.Done()
	}()

	logx.As().Info().
		Str("reason", "SoakStarted").
		Str("node_id", req.NodeID).
		Str("migration_plan", req.MigrationPlanPath).
		Time("cutover_ts", req.CutoverTimestamp).
		Msg("Soak watcher started")

	sw.soakStatus.Store(&SoakStatusResponse{Active: true, Request: &req})

	// Story #520: poll soak criteria, emit JSONL events, auto-decommission.
	<-ctx.Done()
}

// resumeIfNeeded reads cutover-state.jsonl on startup and re-activates the
// watcher if a migration soak was in progress before a daemon restart.
// Stub — implemented in story #520.
//
// Invariant for story #520: before spawning run(), this function must:
//  1. call sw.soakActive.Store(true) — keeps the duplicate-watcher guard consistent
//  2. call sw.soakWg.Add(1) before the goroutine starts — keeps soakWg.Wait()
//     quiescence guarantee intact
//
// Omitting either step is a silent correctness bug.
func (sw *SoakWatcher) resumeIfNeeded(_ context.Context) {
	logx.As().Debug().Msg("Checking for soak state to resume (not yet implemented)")
}
