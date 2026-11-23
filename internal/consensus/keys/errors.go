// SPDX-License-Identifier: Apache-2.0

package keys

import "github.com/joomcode/errorx"

// ErrNoKeySecret is returned by ResolveSecret when a node has no key secret of the
// requested type. Callers that treat a missing secret as optional branch on it.
var ErrNoKeySecret = errorx.IllegalState.NewSubtype("no_key_secret")

// IsNoKeySecret reports whether err is (or wraps) an ErrNoKeySecret error.
func IsNoKeySecret(err error) bool {
	return errorx.IsOfType(err, ErrNoKeySecret)
}
