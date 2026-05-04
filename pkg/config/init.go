// SPDX-License-Identifier: Apache-2.0

package config

import (
	"sync"

	"github.com/automa-saga/logx"
)

var once sync.Once

func init() {
	Init()
}

func Init() {
	once.Do(func() {
		_ = logx.Initialize(globalConfig.Log)
	})
}
