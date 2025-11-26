package consensuscmd

import (
	"os"
	"testing"

	"github.com/automa-saga/logx"
)

func TestMain(m *testing.M) {
	// initialize logging once for all tests
	_ = logx.Initialize(logx.LoggingConfig{
		Level:          "debug",
		ConsoleLogging: true,
	})
	os.Exit(m.Run())
}
