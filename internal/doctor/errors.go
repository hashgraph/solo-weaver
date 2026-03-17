// SPDX-License-Identifier: Apache-2.0

package doctor

import "github.com/joomcode/errorx"

func init() {
}

var (
	ErrPropertyResolution = errorx.RegisterProperty("resolution")
	ErrNamespace          = errorx.NewNamespace("doctor")
	NotFoundError         = ErrNamespace.NewType("not_found", errorx.NotFound())
)
