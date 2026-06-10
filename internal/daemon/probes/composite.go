// SPDX-License-Identifier: Apache-2.0

package probes

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// CompositeProbe fans out to a set of leaf Probe instances concurrently.
// It returns nil only when every sub-probe passes. The first failure cancels
// sibling probes via errgroup context cancellation so the composite exits as
// fast as possible.
//
// Sub-probes may themselves be CompositeProbe instances — since CompositeProbe
// satisfies the Probe interface, probes can be nested to arbitrary depth.
//
// This type lives in the probes package (not daemon) so that sub-packages such
// as consensus can compose multiple probes inside RequiredProbe() without
// creating an import cycle with the parent daemon package.
//
// Use NewCompositeProbe to construct.
type CompositeProbe struct {
	probes []Probe
}

// NewCompositeProbe returns a CompositeProbe that runs all provided probes
// concurrently.
func NewCompositeProbe(probes ...Probe) *CompositeProbe {
	return &CompositeProbe{probes: probes}
}

// Probe implements the Probe interface.
func (c *CompositeProbe) Probe(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	for _, p := range c.probes {
		p := p // pin loop variable
		eg.Go(func() error { return p.Probe(ctx) })
	}
	return eg.Wait()
}
