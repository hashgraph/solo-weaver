// SPDX-License-Identifier: Apache-2.0

package models

import "github.com/joomcode/errorx"

var (
	// ErrPropertyResolution is the errorx property key used to attach remediation
	// hints to precondition errors — mirrors doctor.ErrPropertyResolution without
	// creating a circular import.
	ErrPropertyResolution = errorx.RegisterProperty("resolution")

	// ErrPropertyReason is the errorx property key used to attach a stable,
	// machine-readable reason code to an error (e.g. "UpgradeDirOwnershipCheckFailed").
	// Mirrors the log-line reason= field convention so structured tooling can
	// correlate /status JSON output with journal entries.
	ErrPropertyReason = errorx.RegisterProperty("reason")

	// ErrPropertyWhyFloor is the errorx property key used to attach the rule
	// attribution string ("Why:") that produced the binding hardware floor for a
	// failed hardware check. Set by hardware check workflow steps; consumed by
	// doctor.checkErrCompact to display "Set by: <reason>" in the error panel.
	ErrPropertyWhyFloor = errorx.RegisterProperty("whyFloor")
)
