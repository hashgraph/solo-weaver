package bll

import (
	"context"
	"sync"
	"time"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/joomcode/errorx"
	"golang.org/x/sync/singleflight"
)

// MaxStateStaleDuration defines the maximum allowed staleness duration for internal state before it's considered stale
// and needs to be refreshed from reality before making decisions.
const MaxStateStaleDuration = 10 * time.Minute

// BLL is the interface for the business intelligence & logic layer. It acts as the main orchestrator for
// checking intents and executing workflows to meet the intents.
//
// It maintains the current state and default configurations internally and ensures it's reasonably fresh before making
// decisions and executing appropriate workflows.
//
// It doesn't return the workflow directly because it needs to ensure that it's internal state is reasonably fresh and
// remains up to date after executing an intent.
//
// It decouples the high-level intent checking and execution logic from the low-level state management and reality checking.
// Therefore, the same business level operations can be performed by CLI or API layers without duplicating the logic.
type BLL interface {
	// Check checks if the intent is allowed or not given the parameters and internal state
	Check(intent *core.Intent) error

	// Execute runs the workflow required to meet the intent or return error.
	// If intent is allowed, it would run appropriate workflow and return the report where report may have error
	// If intent is not allowed, it would return nil report and an error explaining why intent isn't allowed
	// It invokes Check before executing the intent. It also invokes CheckReality to ensure it's internal state is
	// up to date within the MaxStateStaleDuration time.
	Execute(ctx context.Context, intent *core.Intent) (*automa.Report, error)

	// CurrentState returns a copy of the current (or latest) cached runtime state
	CurrentState() *core.State

	// CheckReality forces the internal state to be synced with reality if it hasn't been refreshed recently.
	// Note that during normal operation, BLL synchronizes as soon as it executes an Intent. However, user may invoke it
	// specifically to synchronize the state
	CheckReality(ctx context.Context) error

	// CheckRealityWithInterval ensures current state is synced and withing the maxInterval threshold
	CheckRealityWithInterval(ctx context.Context, maxInterval time.Duration) error
}

// bll is the implementation of BLL interface
type bll struct {
	mu sync.RWMutex
	sf singleflight.Group

	conf             *config.Config      // conf will provide default values for Intent parameters if parameter is missing
	stateManager     core.StateManager   // maintains current state synced with disk
	realityChecker   core.RealityChecker // ensures state is synced with realityChecker
	realityCheckedAt *time.Time          // last time current state is refreshed
}

func (b *bll) Check(intent *core.Intent) error {
	if b == nil {
		return errorx.IllegalState.New("BLL is nil")
	}
	if intent == nil {
		return errorx.IllegalArgument.New("intent is nil")
	}
	if b.stateManager == nil {
		return errorx.IllegalState.New("state manager isn't initialized")
	}

	// Ensure the cached state is reasonably fresh before making a decision.
	// Use background context because Check() has no context parameter.
	if err := b.checkRealityWithOptions(context.Background(), false, MaxStateStaleDuration); err != nil {
		return err
	}

	// TODO: perform actual intent validation against current state.
	// For now, basic validation passed.
	return nil
}

func (b *bll) Execute(ctx context.Context, intent *core.Intent) (*automa.Report, error) {
	if b == nil {
		return nil, errorx.IllegalState.New("BLL is nil")
	}
	if err := b.Check(intent); err != nil {
		return nil, err
	}

	// Ensure state is synced within the standard interval before execution.
	if err := b.checkRealityWithOptions(ctx, false, MaxStateStaleDuration); err != nil {
		return nil, err
	}

	// TODO: actual execution implementation goes here.
	return nil, errorx.NotImplemented.New("Execute not implemented")
}

func (b *bll) CurrentState() *core.State {
	if b == nil || b.stateManager == nil {
		return nil
	}
	st := b.stateManager.State()
	if st == nil {
		return nil
	}
	return st.Clone() // return a cloned copy
}

func (b *bll) CheckReality(ctx context.Context) error {
	return b.checkRealityWithOptions(ctx, false, MaxStateStaleDuration)
}

func (b *bll) CheckRealityWithInterval(ctx context.Context, maxInterval time.Duration) error {
	return b.checkRealityWithOptions(ctx, false, maxInterval)
}

// checkRealityWithOptions allows more control on actual check reality operation.
// Holding the whole method under a write lock blocks other readers (e.g., CurrentState callers) for the duration of the
// Read-lock + write-lock (or read-check then upgrade) is used to avoid holding a heavy exclusive lock while doing
// I/O-bound work (the refresh).
// refresh and reduces concurrency.
// It uses a read lock for the quick staleness check, then perform the refresh without locks.
// To avoid duplicate concurrent refreshes, it uses a single-flight deduplication so only one goroutine refreshes while
// others wait for the result.
func (b *bll) checkRealityWithOptions(ctx context.Context, force bool, maxRefreshInterval time.Duration) error {
	if b == nil || b.realityChecker == nil {
		return errorx.IllegalState.New("RealityChecker isn't set, unable to check reality.")
	}

	now := time.Now()

	// Fast read-locked check: cheap and non-blocking for readers.
	b.mu.RLock()
	last := b.realityCheckedAt
	b.mu.RUnlock()

	needRefresh := force
	if !needRefresh {
		if last == nil {
			needRefresh = true
		} else if maxRefreshInterval > 0 && now.Sub(*last) >= maxRefreshInterval {
			needRefresh = true
		}
	}
	if !needRefresh {
		return nil
	}

	// Deduplicate concurrent refreshes so only one goroutine performs the work.
	_, err, _ := b.sf.Do("refresh-state", func() (interface{}, error) {
		// Perform actual refresh without holding locks.
		if err := b.realityChecker.RefreshState(ctx, b.stateManager.State()); err != nil {
			return nil, err
		}

		// Record last-checked time under write lock.
		t := time.Now()
		b.mu.Lock()
		b.realityCheckedAt = &t
		b.mu.Unlock()
		return nil, nil
	})

	return err
}

// buildWorkflow builds the workflow based on the intent
// It is a thin layer that delegates to the appropriate workflow builder
func (b *bll) buildWorkflow(intent *core.Intent) (*automa.WorkflowBuilder, error) {
	// Placeholder implementation
	return nil, errorx.NotImplemented.New("buildWorkflow is not implemented yet")
}

// New creates a new instance of BLL
// It ensures the provided state as well as config are cloned to avoid external mutation.
func New(conf *config.Config, state *core.State, reality core.RealityChecker) (BLL, error) {
	sm, err := core.NewStateManager(core.WithState(state.Clone())) // ensure state is cloned
	if err != nil {
		return nil, err
	}

	return &bll{
		conf:           conf.Clone(), // make a deep copy
		stateManager:   sm,
		realityChecker: reality,
	}, nil
}
