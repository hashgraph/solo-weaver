// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"
	"time"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

// DefaultRefreshInterval is the default duration after which the resolver should refresh its state from the reality checker.
// This is a balance between ensuring reasonably fresh state and avoiding excessive calls to the reality checker,
// which may be expensive or rate-limited.
const DefaultRefreshInterval = 10 * time.Minute

// DefaultRefreshTimeout is the default timeout for state refresh operations, which may involve calls to external
// systems and should not be allowed to run indefinitely.
const DefaultRefreshTimeout = 60 * time.Second

type Resolver[S any, I any] interface {
	WithIntent(intent models.Intent) Resolver[S, I]
	WithUserInputs(inputs I) Resolver[S, I]
	WithDefaults(cfg models.Config) Resolver[S, I]
	WithConfig(cfg models.Config) Resolver[S, I]
	WithEnv(cfg models.Config) Resolver[S, I]
	WithState(blockNodeState S) Resolver[S, I]
	RefreshState(ctx context.Context, force bool) error
	CurrentState() (S, error)
}
