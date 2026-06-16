// SPDX-License-Identifier: Apache-2.0

package daemonkit

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// Probe is the minimal leaf interface for a single prerequisite check.
// Concrete implementations (e.g. a disk-permission or RBAC probe) satisfy this
// interface. Probe should block and retry internally until success or ctx
// cancellation; returning ctx.Err() on cancellation is the expected exit path.
type Probe interface {
	Probe(ctx context.Context) error
}

// ComponentProbe is the component-boundary interface seen by the supervisor.
// A component with no external dependencies sets its probe field to nil and is
// treated as immediately ready by the composite probe runner.
type ComponentProbe interface {
	Probe(ctx context.Context) error

	// ComponentName returns the component identifier used in structured log
	// entries (e.g. "consensus-node").
	ComponentName() string
}

// ProbableMonitor is optionally implemented by monitors that require external
// resources to be verified before they run. RequiredProbe returns a single Probe
// representing everything the monitor needs.
//
// The component automatically collects RequiredProbe() from every enabled
// ProbableMonitor and combines them into its CompositeProbe via BuildComponentProbe.
type ProbableMonitor interface {
	MonitorRunner
	RequiredProbe() Probe
}

// CompositeProbe implements ComponentProbe at the component boundary. It fans
// out to a set of leaf Probe instances concurrently and returns nil only when
// every sub-probe passes. The first failure cancels sibling probes via errgroup
// context cancellation so the composite exits as fast as possible.
//
// Sub-probes may themselves be CompositeProbe instances — since CompositeProbe
// satisfies the Probe interface, probes can be nested to arbitrary depth.
//
// Use NewCompositeProbe to construct.
type CompositeProbe struct {
	name   string
	probes []Probe
}

// NewCompositeProbe returns a CompositeProbe that runs all provided leaf probes
// concurrently under the given component name.
func NewCompositeProbe(componentName string, leafProbes ...Probe) *CompositeProbe {
	return &CompositeProbe{name: componentName, probes: leafProbes}
}

// ComponentName implements ComponentProbe.
func (c *CompositeProbe) ComponentName() string { return c.name }

// Probe implements ComponentProbe (and the Probe interface). It fans out to all
// sub-probes concurrently; the first failure cancels the rest via the errgroup
// context.
func (c *CompositeProbe) Probe(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	for _, p := range c.probes {
		p := p // pin loop variable
		eg.Go(func() error { return p.Probe(ctx) })
	}
	return eg.Wait()
}

// BuildComponentProbe collects RequiredProbe() from every ProbableMonitor in
// monitors and wraps them in a CompositeProbe named componentName. Returns nil
// when no monitor declares a prerequisite (host-only component); the supervisor
// treats a nil probe as immediately ready.
func BuildComponentProbe(componentName string, monitors []MonitorRunner) ComponentProbe {
	var leafProbes []Probe
	for _, m := range monitors {
		if pm, ok := m.(ProbableMonitor); ok {
			leafProbes = append(leafProbes, pm.RequiredProbe())
		}
	}
	if len(leafProbes) == 0 {
		return nil
	}
	return NewCompositeProbe(componentName, leafProbes...)
}
