// SPDX-License-Identifier: Apache-2.0

package probes

import "context"

// Probe is the minimal leaf interface for a single prerequisite check.
// Concrete implementations (KubeRBACProbe, DiskPermissionProbe, …) satisfy
// this interface. Probe should block and retry internally until success or ctx
// cancellation; returning ctx.Err() on cancellation is the expected exit path.
//
// This interface is defined here (in the probes package) rather than in the
// parent daemon package so that consensus and other sub-packages can reference
// it without creating an import cycle.
type Probe interface {
	Probe(ctx context.Context) error
}
