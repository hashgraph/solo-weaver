// SPDX-License-Identifier: Apache-2.0

package daemon

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/daemon/probes"
)

// Probe is a type alias for probes.Probe — re-exported here so callers within
// the daemon package can use daemon.Probe without importing the probes sub-package.
type Probe = probes.Probe

// ComponentProbe is the component-boundary interface seen by the supervisor.
// Only CompositeProbe needs to implement this; leaf Probe implementations do not.
// A component with no external dependencies sets its probe field to nil and is
// treated as immediately ready by runCompositeProbe.
type ComponentProbe interface {
	Probe(ctx context.Context) error

	// ComponentName returns the component identifier used in structured log
	// entries and the /status response (e.g. "consensus-node").
	ComponentName() string
}

// ProbableMonitor is optionally implemented by monitors that require external
// resources to be verified before they run. RequiredProbe returns a single Probe
// representing everything the monitor needs — a leaf probe (e.g. KubeRBACProbe)
// or a CompositeProbe when multiple prerequisites must all pass.
//
// The component automatically collects RequiredProbe() from every enabled
// ProbableMonitor and combines them into its CompositeProbe. Disabling a
// monitor therefore automatically removes its prerequisites from the startup
// probe set.
type ProbableMonitor interface {
	MonitorRunner
	RequiredProbe() Probe
}

// CompositeProbe implements ComponentProbe at the component boundary by naming
// a probes.CompositeProbe. The fan-out logic lives in probes.CompositeProbe so
// that sub-packages (e.g. consensus) can compose multiple Probes inside
// RequiredProbe() using probes.NewCompositeProbe without importing this
// (daemon) package and creating an import cycle.
//
// Sub-probes may themselves be probes.CompositeProbe instances — since that
// type satisfies the Probe interface the nesting is arbitrarily deep.
//
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
