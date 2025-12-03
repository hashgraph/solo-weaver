// SPDX-License-Identifier: Apache-2.0

package config

import "github.com/automa-saga/logx"

func init() {
	// initialize logging with defaults
	_ = logx.Initialize(globalConfig.Log)
}
