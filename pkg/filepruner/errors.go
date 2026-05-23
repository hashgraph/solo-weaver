// SPDX-License-Identifier: Apache-2.0

package filepruner

import "github.com/joomcode/errorx"

var (
	ErrNamespace   = errorx.NewNamespace("filepruner")
	ErrPruneFailed = ErrNamespace.NewType("prune_failed")
	ErrNoTimestamp = ErrNamespace.NewType("no_timestamp")
)
