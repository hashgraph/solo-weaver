// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"sync"
	"time"

	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

// TeleportRuntimeResolver manages the teleport state lifecycle.
// It uses TeleportNodeInputs as the generic input type since the node agent
// is the primary binary-based component that needs state management.
type TeleportRuntimeResolver struct {
	mu              sync.Mutex
	cfg             *models.Config
	state           *state.TeleportState
	refreshInterval time.Duration
	realityChecker  reality.Checker[state.TeleportState]

	intent *models.Intent
	inputs *models.TeleportNodeInputs
}

func (t *TeleportRuntimeResolver) WithIntent(intent models.Intent) Resolver[state.TeleportState, models.TeleportNodeInputs] {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.intent = &intent
	return t
}

func (t *TeleportRuntimeResolver) WithUserInputs(inputs models.TeleportNodeInputs) Resolver[state.TeleportState, models.TeleportNodeInputs] {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inputs = &inputs
	return t
}

func (t *TeleportRuntimeResolver) WithConfig(cfg models.Config) Resolver[state.TeleportState, models.TeleportNodeInputs] {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cfg = &cfg
	return t
}

func (t *TeleportRuntimeResolver) WithState(st state.TeleportState) Resolver[state.TeleportState, models.TeleportNodeInputs] {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = &st
	return t
}

func (t *TeleportRuntimeResolver) RefreshState(ctx context.Context, force bool) error {
	now := htime.Now()

	if !force {
		t.mu.Lock()
		if t.state != nil {
			if now.Sub(t.state.LastSync) < t.refreshInterval {
				t.mu.Unlock()
				return nil
			}
		}
		t.mu.Unlock()
	}

	st, err := t.realityChecker.RefreshState(ctx)
	if err != nil {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = &st
	return nil
}

func (t *TeleportRuntimeResolver) CurrentState() (state.TeleportState, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.state == nil {
		return state.TeleportState{}, errorx.IllegalState.New("teleport state is not initialized")
	}

	return *t.state, nil
}

// NewTeleportRuntimeResolver creates a TeleportRuntimeResolver.
func NewTeleportRuntimeResolver(
	cfg models.Config,
	teleportState state.TeleportState,
	realityChecker reality.Checker[state.TeleportState],
	refreshInterval time.Duration,
) (Resolver[state.TeleportState, models.TeleportNodeInputs], error) {
	return &TeleportRuntimeResolver{
		cfg:             &cfg,
		state:           &teleportState,
		refreshInterval: refreshInterval,
		realityChecker:  realityChecker,
	}, nil
}
