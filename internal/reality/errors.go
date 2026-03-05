// SPDX-License-Identifier: Apache-2.0

package reality

import "github.com/joomcode/errorx"

var ErrNamespace = errorx.NewNamespace("reality")
var ErrFlushError = errorx.NewType(ErrNamespace, "flush_error")
