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
	"github.com/hashgraph/solo-weaver/pkg/models"
)

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

// ── capturingAccessor ─────────────────────────────────────────────────────────

// capturingAccessor wraps an rslAccessor and intercepts per-field calls so
// that the caller (e.g. InstallHandler) can capture each *automa.EffectiveValue
// for post-resolution guard checks, without exposing the internal effective
// values through the public API.
type capturingAccessor struct {
	inner          rslAccessor
	releaseNameFn  func() (*automa.EffectiveValue[string], error)
	versionFn      func() (*automa.EffectiveValue[string], error)
	namespaceFn    func() (*automa.EffectiveValue[string], error)
	chartNameFn    func() (*automa.EffectiveValue[string], error)
	chartRepoFn    func() (*automa.EffectiveValue[string], error)
	chartVersionFn func() (*automa.EffectiveValue[string], error)
}

var _ rslAccessor = (*capturingAccessor)(nil)

func (c *capturingAccessor) ReleaseName() (*automa.EffectiveValue[string], error) {
	if c.releaseNameFn != nil {
		return c.releaseNameFn()
	}
	return c.inner.ReleaseName()
}
func (c *capturingAccessor) Version() (*automa.EffectiveValue[string], error) {
	if c.versionFn != nil {
		return c.versionFn()
	}
	return c.inner.Version()
}
func (c *capturingAccessor) Namespace() (*automa.EffectiveValue[string], error) {
	if c.namespaceFn != nil {
		return c.namespaceFn()
	}
	return c.inner.Namespace()
}
func (c *capturingAccessor) ChartName() (*automa.EffectiveValue[string], error) {
	if c.chartNameFn != nil {
		return c.chartNameFn()
	}
	return c.inner.ChartName()
}
func (c *capturingAccessor) ChartRepo() (*automa.EffectiveValue[string], error) {
	if c.chartRepoFn != nil {
		return c.chartRepoFn()
	}
	return c.inner.ChartRepo()
}
func (c *capturingAccessor) ChartVersion() (*automa.EffectiveValue[string], error) {
	if c.chartVersionFn != nil {
		return c.chartVersionFn()
	}
	return c.inner.ChartVersion()
}
func (c *capturingAccessor) Storage() (*automa.EffectiveValue[models.BlockNodeStorage], error) {
	return c.inner.Storage()
}
