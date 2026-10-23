// SPDX-License-Identifier: Apache-2.0

package state

import "github.com/joomcode/errorx"

func init() {
}

var (
	ErrNamespace  = errorx.NewNamespace("state")
	NotFoundError = ErrNamespace.NewType("not_found", errorx.NotFound())
	// EntryNotAppendedError is a write whose state files landed but whose
	// action history entry could not be appended: the state is saved, its
	// record is not.
	EntryNotAppendedError = errorx.IllegalState.NewSubtype("entry_not_appended")
)
