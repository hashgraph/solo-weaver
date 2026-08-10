// SPDX-License-Identifier: Apache-2.0

// Package reasons holds the shared errx reason vocabulary. It is a leaf package
// (errx only) so that any package can import it without creating a cycle.
package reasons

import "github.com/automa-saga/errx"

// Reason codes with live call sites, read back via errx.ReasonOf.
const (
	// InvalidArgument - a flag value, flag combination or positional argument was rejected.
	InvalidArgument errx.Reason = "InvalidArgument"

	// FileMissing - a required file or directory does not exist and we will not create it.
	FileMissing errx.Reason = "FileMissing"

	// PermissionDenied - superuser privilege required, or file/resource access denied.
	PermissionDenied errx.Reason = "PermissionDenied"

	// Internal - an invariant breach or a bug in solo-weaver, not an operator error.
	Internal errx.Reason = "Internal"

	// NotInstalled - a component is absent; the operator must run its install command first.
	NotInstalled errx.Reason = "NotInstalled"

	// PreconditionNotMet - the system is in the wrong state for this command; change the
	// state or use a different verb.
	PreconditionNotMet errx.Reason = "PreconditionNotMet"
)
