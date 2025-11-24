package config

import "github.com/automa-saga/logx"

func init() {
	// initialize logging with defaults
	_ = logx.Initialize(config.Log)
}
