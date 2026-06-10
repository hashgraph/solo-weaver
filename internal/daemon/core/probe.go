// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/daemon/probes"
)

// Probe is a type alias for probes.Probe — re-exported here so callers can use
// core.Probe without importing the probes sub-package directly.
type Probe = probes.Probe

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

// CompositeProbe implements ComponentProbe at the component boundary.
// Use NewCompositeProbe to construct.
type CompositeProbe struct {
	name  string
	inner *probes.CompositeProbe
}

// NewCompositeProbe returns a CompositeProbe that runs all provided leaf probes
// concurrently under the given component name.
func NewCompositeProbe(componentName string, leafProbes ...Probe) *CompositeProbe {
	return &CompositeProbe{name: componentName, inner: probes.NewCompositeProbe(leafProbes...)}
}

// ComponentName implements ComponentProbe.
func (c *CompositeProbe) ComponentName() string { return c.name }

// Probe implements ComponentProbe. Delegates to the inner probes.CompositeProbe
// which fans out to all sub-probes concurrently via errgroup.
func (c *CompositeProbe) Probe(ctx context.Context) error { return c.inner.Probe(ctx) }

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
