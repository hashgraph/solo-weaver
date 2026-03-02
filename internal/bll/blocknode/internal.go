// SPDX-License-Identifier: Apache-2.0

package blocknode

// internal.go contains the private interface and step-adapter shims that
// decouple the handler files from concrete rsl and step types.
//
// rslAccessor abstracts the rsl layer so handlers can be unit-tested by
// injecting a stub without constructing a real registry.
//
// The step adapters (setupBlockNode, upgradeBlockNode, …) are thin wrappers
// so that handler files import only this package rather than reaching directly
// into internal/workflows/steps.

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// errPropertyResolution is the errorx property key used to attach remediation
// hints to precondition errors — mirrors doctor.ErrPropertyResolution without
// creating a circular import.
var errPropertyResolution = errorx.RegisterProperty("bll.resolution_hint")

// ── rslAccessor ───────────────────────────────────────────────────────────────

// rslAccessor exposes only the field-resolution methods that block-node
// handlers actually need.  Using per-field methods (rather than returning
// *rsl.BlockNodeRuntimeState) keeps the interface narrow and makes it
// trivially stubbable in unit tests.
type rslAccessor interface {
	ReleaseName() (*automa.EffectiveValue[string], error)
	Version() (*automa.EffectiveValue[string], error)
	Namespace() (*automa.EffectiveValue[string], error)
	ChartName() (*automa.EffectiveValue[string], error)
	ChartRepo() (*automa.EffectiveValue[string], error)
	ChartVersion() (*automa.EffectiveValue[string], error)
	Storage() (*automa.EffectiveValue[models.BlockNodeStorage], error)
}

// registryAccessor wraps *rsl.BlockNodeRuntimeState to satisfy rslAccessor.
// It is constructed from the Registry in NewHandler and passed into each
// per-action handler.
type registryAccessor struct{ bn *rsl.BlockNodeRuntimeState }

// compile-time proof that registryAccessor implements rslAccessor.
var _ rslAccessor = registryAccessor{}

func (r registryAccessor) ReleaseName() (*automa.EffectiveValue[string], error) {
	return r.bn.ReleaseName()
}
func (r registryAccessor) Version() (*automa.EffectiveValue[string], error) {
	return r.bn.Version()
}
func (r registryAccessor) Namespace() (*automa.EffectiveValue[string], error) {
	return r.bn.Namespace()
}
func (r registryAccessor) ChartName() (*automa.EffectiveValue[string], error) {
	return r.bn.ChartName()
}
func (r registryAccessor) ChartRepo() (*automa.EffectiveValue[string], error) {
	return r.bn.ChartRepo()
}
func (r registryAccessor) ChartVersion() (*automa.EffectiveValue[string], error) {
	return r.bn.ChartVersion()
}
func (r registryAccessor) Storage() (*automa.EffectiveValue[models.BlockNodeStorage], error) {
	return r.bn.Storage()
}

// ── Step adapters ─────────────────────────────────────────────────────────────
// These thin wrappers allow handler files to call a local function rather than
// reaching directly into two separate sub-packages.

func setupBlockNode(ins models.BlocknodeInputs) *automa.WorkflowBuilder {
	return steps.SetupBlockNode(ins)
}

func upgradeBlockNode(ins models.BlocknodeInputs) *automa.WorkflowBuilder {
	return steps.UpgradeBlockNode(ins)
}

func resetBlockNode(ins models.BlocknodeInputs) *automa.WorkflowBuilder {
	return steps.ResetBlockNode(ins)
}

func uninstallBlockNode(ins models.BlocknodeInputs) *automa.WorkflowBuilder {
	return steps.UninstallBlockNode(ins)
}

func purgeBlockNodeStorage(ins models.BlocknodeInputs) *automa.WorkflowBuilder {
	return steps.PurgeBlockNodeStorage(ins)
}

func installClusterWorkflow(nodeType string, profile string, skipHW bool, sm state.Manager) *automa.WorkflowBuilder {
	return workflows.InstallClusterWorkflow(nodeType, profile, skipHW, sm)
}
