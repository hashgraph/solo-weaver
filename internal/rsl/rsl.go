// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"context"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

type Resolver[S any, I any] interface {
	WithUserInputs(inputs I) *Base[S, I]
	WithConfig(cfg models.Config) *Base[S, I]
	WithState(blockNodeState S) *Base[S, I]
	RefreshState(ctx context.Context, force bool) error
	CurrentState() (S, error)
}
