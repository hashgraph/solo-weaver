// SPDX-License-Identifier: Apache-2.0

package eventlog

import "github.com/joomcode/errorx"

var (
	ErrNamespace    = errorx.NewNamespace("eventlog")
	ErrInvalidEvent = ErrNamespace.NewType("invalid_event")
)
