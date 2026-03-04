// SPDX-License-Identifier: Apache-2.0

package config

import (
	"sync"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/security"
)

var once sync.Once

func init() {
	Init()
}

func Init() {
	once.Do(func() {
		security.SetServiceAccount(ServiceAccount())

		// initialize logging with defaults
		_ = logx.Initialize(globalConfig.Log)
	})
}
