// SPDX-License-Identifier: Apache-2.0

package models

import (
	"github.com/hashgraph/solo-weaver/pkg/models"
)

var (
	pp = models.NewWeaverPaths(models.DefaultWeaverHome)
)

func Paths() models.WeaverPaths {
	return *pp
}
