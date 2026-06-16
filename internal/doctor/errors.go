// SPDX-License-Identifier: Apache-2.0

package doctor

import (
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

func init() {
}

var (
	// ErrPropertyResolution is the canonical errorx property key for attaching
	// human-readable remediation hints to errors. Aliased from pkg/models so that
	// errors set by any package (workflow steps, config loaders, etc.) are found
	// by findResolution regardless of which package attached the property.
	ErrPropertyResolution = models.ErrPropertyResolution
	ErrNamespace          = errorx.NewNamespace("doctor")
	NotFoundError         = ErrNamespace.NewType("not_found", errorx.NotFound())
)
