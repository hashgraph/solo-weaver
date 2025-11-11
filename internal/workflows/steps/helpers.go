package steps

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/joomcode/errorx"
)

// runCmd runs a bash command and returns its trimmed output or an error
var runCmd = func(script string) (string, error) {
	out, err := exec.Command("bash", "-c", script).Output()
	if err != nil {
		return "", errorx.IllegalState.Wrap(err, "failed to execute bash command: %s", script)
	}
	val := strings.TrimSpace(string(out))
	return val, nil
}

// Sleep sleeps for the given duration or returns early if the context
// is canceled or its deadline expires. Returns nil on success or ctx.Err() on cancellation.
func Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
