// SPDX-License-Identifier: Apache-2.0

package bll

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// Option is the type of functional options for configuring handlers.  It is a
// function that takes a pointer to the handler and modifies it, returning an error
// if the option is invalid or cannot be applied.
type Option[T any] func(*T) error

// ── ActionHandler ─────────────────────────────────────────────────────────────

// ActionHandler is the contract every per-action, per-node-type handler must
// satisfy.  [I any] is the node-specific inputs struct (e.g. models.BlocknodeInputs).
//
// Splitting at this boundary means:
//   - Each handler is independently unit-testable with zero routing boilerplate.
//   - Adding a new action (e.g. ActionMigrate) is a new file, not a new switch arm.
//   - Adding a new node type (MirrorNode) is a new package, not a new God struct.
type ActionHandler[I any] interface {
	// PrepareEffectiveInputs resolves the winning value for each field from the
	// three sources (config default, current deployed state, user input) and
	// applies all field-level validators (immutability, override guards, etc.).
	// The returned inputs are fully resolved and safe to pass to BuildWorkflow.
	PrepareEffectiveInputs(inputs *models.UserInputs[I]) (*models.UserInputs[I], error)

	// BuildWorkflow validates action-level preconditions (e.g. "must be deployed
	// before upgrade") and returns the ready-to-execute WorkflowBuilder.
	BuildWorkflow(
		nodeState state.BlockNodeState,
		clusterState state.ClusterState,
		inputs *models.UserInputs[I],
	) (*automa.WorkflowBuilder, error)
}
