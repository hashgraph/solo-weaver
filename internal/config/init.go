// SPDX-License-Identifier: Apache-2.0

package config

import (
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/security"
)

func init() {
	security.SetServiceAccount(svcAcc)

	// initialize logging with defaults
	_ = logx.Initialize(globalConfig.Log)
}
