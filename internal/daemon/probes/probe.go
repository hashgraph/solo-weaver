// SPDX-License-Identifier: Apache-2.0

package probes

import "github.com/hashgraph/solo-weaver/pkg/daemonkit"

// Probe is a type alias for daemonkit.Probe — re-exported here so the leaf probe
// implementations and their callers can reference probes.Probe while remaining
// assignment-compatible with the daemonkit supervisor's probe wiring.
//
// This alias is defined here (in the probes package) rather than forcing every
// caller to import daemonkit directly so that consensus and other sub-packages
// can compose probes without an extra import. The leaf implementations and the
// pkg/models coupling move out of this package in a follow-up issue.
type Probe = daemonkit.Probe
