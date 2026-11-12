// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"slices"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/joomcode/errorx"
)

// ErrDeferred marks a startup migration that cannot run yet, for example
// because the Kubernetes API is not reachable. The pass skips it, warns, and
// does not record the provisioner version, so the next invocation retries it.
// Outside the startup scope it fails like any other error.
//
// Return it only before Execute changes anything: a deferred migration is
// never rolled back and is not reported as applied.
// Return it as the outermost error, wrapped only by errx.Decorate; under any
// other wrap it fails the pass. A migration that can defer is registered before
// the Cilium agent restart, which holds back only for deferrals it can see.
var ErrDeferred = errorx.DataUnavailable.NewSubtype("migration_deferred")

// Deferral is one migration that deferred in a pass: its id, the operator
// message of its error, and that error's hints.
type Deferral struct {
	ID    string
	Cause string
	Hints []string
}

// Deferred returns the migrations that deferred so far in this pass, in order.
func (c *Context) Deferred() []Deferral {
	return slices.Clone(c.deferred)
}

// deferralCause is the operator text of a deferral: the sentinel's own message
// and what it wraps, without the sentinel's type name.
func deferralCause(err error) string {
	e := errorx.Cast(err)
	if e == nil {
		return doctor.OperatorMessage(err)
	}
	cause := e.Message()
	if c := e.Cause(); c != nil {
		cause += ": " + doctor.OperatorMessage(c)
	}
	return cause
}

// recordDeferral reports whether err defers m in this context's scope and, if
// so, records it.
func (c *Context) recordDeferral(m Migration, err error) bool {
	if c.Component != ScopeStartup || !errorx.IsOfType(err, ErrDeferred) {
		return false
	}
	hints, _ := errx.Hints(err)
	c.deferred = append(c.deferred, Deferral{ID: m.ID(), Cause: deferralCause(err), Hints: hints})
	return true
}
