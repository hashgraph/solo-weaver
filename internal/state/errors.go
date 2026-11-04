// SPDX-License-Identifier: Apache-2.0

package state

import "github.com/joomcode/errorx"

func init() {
}

var (
	ErrNamespace  = errorx.NewNamespace("state")
	NotFoundError = ErrNamespace.NewType("not_found", errorx.NotFound())
)
