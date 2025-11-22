package steps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/state"
	"golang.hedera.com/solo-weaver/pkg/software"
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

func findScript(rel string) string {
	// try absolute/cleaned path first
	if p, err := filepath.Abs(rel); err == nil {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return rel
	}

	dir := wd
	level := 0
	for {
		if level > 5 {
			// prevent too deep search
			break
		}

		// check for script in this dir
		p := filepath.Join(dir, rel)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}

		// if go.mod exists in this dir, treat it as project root and stop here
		// This prevents searching all the way to filesystem root
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// reached filesystem root
			break
		}
		dir = parent
		level++
	}

	// fallback to provided relative path
	return rel
}

// recordInstallState records the installation state for a software installer
func recordInstallState(installer software.Software) {
	stateManager := installer.GetStateManager()
	softwareName := installer.GetSoftwareName()
	version := installer.Version()
	_ = stateManager.RecordState(softwareName, state.TypeInstalled, version)
}

// recordConfigureState records the configuration state for a software installer
func recordConfigureState(installer software.Software) {
	stateManager := installer.GetStateManager()
	softwareName := installer.GetSoftwareName()
	version := installer.Version()
	_ = stateManager.RecordState(softwareName, state.TypeConfigured, version)
}

// removeInstallState removes the installation state for a software installer
func removeInstallState(installer software.Software) {
	stateManager := installer.GetStateManager()
	softwareName := installer.GetSoftwareName()
	_ = stateManager.RemoveState(softwareName, state.TypeInstalled)
}

// removeConfigureState removes the configuration state for a software installer
func removeConfigureState(installer software.Software) {
	stateManager := installer.GetStateManager()
	softwareName := installer.GetSoftwareName()
	_ = stateManager.RemoveState(softwareName, state.TypeConfigured)
}
