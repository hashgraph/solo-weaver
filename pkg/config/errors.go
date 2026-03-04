// SPDX-License-Identifier: Apache-2.0

package config

import "github.com/joomcode/errorx"

func init() {
}

var (
	ErrNamespace  = errorx.NewNamespace("config")
	NotFoundError = ErrNamespace.NewType("not_found", errorx.NotFound())
)
