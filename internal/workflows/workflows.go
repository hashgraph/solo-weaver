package workflows

import (
	"fmt"
	"github.com/automa-saga/automa"
	"runtime"
)

func NewSetupWorkflow() (automa.Builder, error) {
	switch runtime.GOARCH {
	case "debian":
		return SetupDebianOS(), nil
	default:
		return nil, fmt.Errorf("unsupported architecture %q", runtime.GOARCH)
	}
}
