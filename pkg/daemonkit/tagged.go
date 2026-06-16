// SPDX-License-Identifier: Apache-2.0

package daemonkit

import (
	"context"
	"strings"
)

// ProbeError is a kit-native error carrying an operator-facing Reason code and
// Resolution hint as plain struct fields. It mirrors StatusError so that the
// daemon boundary can build a rich StatusError without reaching for an errorx
// property registry — keeping daemonkit free of any pkg/models coupling.
//
// Callers that want doctor-layer styling re-wrap ProbeError into errorx with
// their own property keys at the solo-weaver boundary; the kit itself stays
// dependency-light.
type ProbeError struct {
	// Reason is a stable, machine-readable key (e.g. "UpgradeDirOwnershipCheckFailed").
	Reason string

	// Resolution is an actionable command or instruction the operator should run.
	Resolution string

	// Message is the human-readable error string. When empty, Error() falls back
	// to the wrapped error's message.
	Message string

	// Err is the underlying error that triggered this failure, if any.
	Err error
}

// Error implements error. It prefers Message, falling back to the wrapped error.
func (e *ProbeError) Error() string {
	switch {
	case e.Message != "":
		return e.Message
	case e.Err != nil:
		return e.Err.Error()
	default:
		return e.Reason
	}
}

// Unwrap exposes the underlying error for errors.Is / errors.As traversal.
func (e *ProbeError) Unwrap() error { return e.Err }

// TaggedProbe wraps a leaf Probe and attaches an operator-facing Reason code and
// Resolution hint to any error it returns. Use it inside RequiredProbe()
// implementations so that every prerequisite failure carries context-specific
// guidance for the operator.
//
// On failure it returns a *ProbeError carrying Reason and Resolution as plain
// struct fields (no errorx property registry). The daemon boundary reads those
// fields directly to build a StatusError.
//
// Example:
//
//	&daemonkit.TaggedProbe{
//	    Inner:      &daemonkit.DiskOwnershipProbe{Path: upgradeRoot, ...},
//	    Reason:     "UpgradeRootOwnershipCheckFailed",
//	    Resolution: "sudo chown hedera:hedera " + upgradeRoot,
//	}
type TaggedProbe struct {
	Inner      Probe
	Reason     string
	Resolution string
}

// Probe implements Probe. Delegates to Inner; on failure wraps the error in a
// *ProbeError carrying Reason and Resolution so that the daemon boundary can
// build a rich StatusError without errorx property extraction.
func (p *TaggedProbe) Probe(ctx context.Context) error {
	if err := p.Inner.Probe(ctx); err != nil {
		return &ProbeError{
			Reason:     p.Reason,
			Resolution: p.Resolution,
			Message:    strings.TrimSpace(err.Error()),
			Err:        err,
		}
	}
	return nil
}
