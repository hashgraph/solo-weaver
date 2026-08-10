// SPDX-License-Identifier: Apache-2.0

package models

import (
	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
)

var (
	// ErrPropertyResolution holds remediation hints. Must alias errx's property —
	// errorx compares properties by pointer, so a re-registration is invisible to errx.Hints.
	ErrPropertyResolution = errx.PropertyResolution

	// ErrPropertyReason holds the machine-readable reason code. Prefer errx.WithReason
	// or errx.Decorate over setting it directly. Printable: it renders in err.Error().
	ErrPropertyReason = errx.PropertyReason

	// ErrPropertyWhyFloor is the errorx property key used to attach the rule
	// attribution string ("Why:") that produced the binding hardware floor for a
	// failed hardware check. Set by hardware check workflow steps; consumed by
	// doctor.checkErrCompact to display "Set by: <reason>" in the error panel.
	ErrPropertyWhyFloor = errorx.RegisterProperty("whyFloor")
)
