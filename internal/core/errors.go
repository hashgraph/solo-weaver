package core

import "github.com/joomcode/errorx"

func init() {
}

var (
	ErrNamespace = errorx.NewNamespace("solo-provisioner")

	IllegalArgument = ErrNamespace.NewType("illegal_argument")
	ConfigNotFound  = ErrNamespace.NewType("config_not_found", errorx.NotFound())
)
