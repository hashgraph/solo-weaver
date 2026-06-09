// SPDX-License-Identifier: Apache-2.0

package daemon

import "github.com/joomcode/errorx"

var (
	ErrNamespace = errorx.NewNamespace("daemon")

	// ErrConfig is the base type for daemon configuration errors.
	ErrConfig = ErrNamespace.NewType("config")

	// ErrConfigNotFound is returned when daemon.yaml does not exist at the expected path.
	ErrConfigNotFound = ErrConfig.NewSubtype("not_found", errorx.NotFound())

	// ErrConfigMalformed is returned when daemon.yaml exists but cannot be parsed or has missing required fields.
	ErrConfigMalformed = ErrConfig.NewSubtype("malformed")
)

// IsConfigNotFound reports whether err is (or wraps) an ErrConfigNotFound error.
func IsConfigNotFound(err error) bool {
	return errorx.IsOfType(err, ErrConfigNotFound)
}
