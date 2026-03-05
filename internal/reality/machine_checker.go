// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

// machineChecker probes the local host: software binaries and hardware metrics.
// It depends only on a state.Manager (to read the latest persisted software state)
// and optional path overrides for sandbox bin / state directories.
type machineChecker struct {
	Base
	sandboxBinDir string
	stateDir      string
}

// newMachineChecker constructs a machineChecker.
// sandboxBinDir and stateDir may be empty strings; the checker will fall back
// to models.Paths() values at call time.
func newMachineChecker(sm state.Manager, sandboxBinDir, stateDir string) Checker[state.MachineState] {
	return &machineChecker{
		Base:          Base{sm: sm},
		sandboxBinDir: sandboxBinDir,
		stateDir:      stateDir,
	}
}

func (m *machineChecker) FlushState(st state.MachineState) error {
	if err := m.sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return ErrFlushError.Wrap(err, "failed to refresh state")
	}
	fullState := m.sm.State()
	fullState.MachineState = st
	if err := m.sm.Set(fullState).Flush(); err != nil {
		return ErrFlushError.Wrap(err, "failed to persist state with refreshed MachineState")
	}

	return nil
}

// RefreshState collects current host software and hardware state.
// Software state merges: persisted new state > legacy sidecar files > live binary stat.
func (m *machineChecker) RefreshState(_ context.Context) (state.MachineState, error) {
	// must refresh to get the latest persisted state before merging with live checks;
	// if refresh fails due to missing state (e.g. first run), log and continue with empty state
	if err := m.sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return state.MachineState{}, errorx.IllegalState.Wrap(err, "failed to refresh state")
	}

	current := m.sm.State()
	logx.As().Debug().Any("currentMachineState", current.MachineState).Msg("Loaded current state")

	ms := current.MachineState
	ms.Software = m.refreshSoftwareState(current)
	ms.Hardware = m.refreshHardwareState()
	ms.LastSync = htime.Now()

	if err := m.FlushState(ms); err != nil {
		return ms, err
	}

	logx.As().Debug().Any("machineState", ms).Msg("Refreshed machine state")

	return ms, nil
}

// refreshSoftwareState probes each known binary on the filesystem and merges
// with persisted version/configured metadata from current state.
//
// Source-of-truth priority (highest → lowest):
//  1. New state.yaml MachineState.Software map  (set by DefaultStateManager)
//  2. Legacy sidecar files  <StateDir>/<name>.installed / .configured
//  3. Binary presence on disk  (live filesystem stat)
func (m *machineChecker) refreshSoftwareState(current state.State) map[string]state.SoftwareState {
	result := make(map[string]state.SoftwareState)

	sandboxBinDir := m.sandboxBinDir
	if sandboxBinDir == "" {
		sandboxBinDir = models.Paths().SandboxBinDir
	}
	stateDir := m.stateDir
	if stateDir == "" {
		stateDir = models.Paths().StateDir
	}

	for _, name := range software.KnownSoftwareNames() {
		sw := state.SoftwareState{Name: name}

		// --- Priority 1: carry from new MachineState.Software map ---
		if persisted, ok := current.MachineState.Software[name]; ok && persisted.Name != "" {
			sw = persisted
		} else {
			// --- Priority 2: read legacy sidecar files ---
			sw.Installed, sw.Version = readLegacyStateFiles(stateDir, name, "installed")
			if configured, _ := readLegacyStateFiles(stateDir, name, "configured"); configured {
				sw.Configured = true
			}
		}

		// --- Priority 3: live binary check always overrides Installed ---
		// If binary is absent on disk, force Installed=false regardless of what
		// the state files say (handles manual deletions).
		binPath := filepath.Join(sandboxBinDir, name)
		if _, err := os.Stat(binPath); err == nil {
			sw.Installed = true
		} else {
			sw.Installed = false
			logx.As().Debug().
				Str("binary", binPath).
				Msg("Binary not found on filesystem — marking as not installed")
		}

		sw.LastSync = htime.Now()
		result[name] = sw
	}

	return result
}

// readLegacyStateFiles reads a legacy <name>.<stateType> state file from stateDir.
// It returns (true, version) when the file exists, (false, "") otherwise.
// The file content is expected to be in the format written by state.Manager.RecordState:
//
//	"installed at version 1.30.0\n"
//	"configured at version 1.30.0\n"
func readLegacyStateFiles(stateDir, name, stateType string) (exists bool, version string) {
	filePath := filepath.Join(stateDir, name+"."+stateType)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false, ""
	}

	line := strings.TrimSpace(string(data))
	const marker = " at version "
	if idx := strings.Index(line, marker); idx != -1 {
		version = strings.TrimSpace(line[idx+len(marker):])
	}

	return true, version
}

// refreshHardwareState collects current host hardware metrics.
func (m *machineChecker) refreshHardwareState() map[string]state.HardwareState {
	logx.As().Debug().Msg("Probing hardware state using hardware.GetHostProfile()")
	result := make(map[string]state.HardwareState)
	now := htime.Now()

	hp := hardware.GetHostProfile()

	result["os"] = state.HardwareState{
		Type:     "os",
		Info:     fmt.Sprintf("%s %s", hp.GetOSVendor(), hp.GetOSVersion()),
		LastSync: now,
	}

	result["cpu"] = state.HardwareState{
		Type:     "cpu",
		Count:    int(hp.GetCPUCores()),
		LastSync: now,
	}

	result["memory"] = state.HardwareState{
		Type:     "memory",
		Size:     fmt.Sprintf("%d GB", hp.GetTotalMemoryGB()),
		Info:     fmt.Sprintf("%d GB available", hp.GetAvailableMemoryGB()),
		LastSync: now,
	}

	result["storage"] = state.HardwareState{
		Type:     "storage",
		Size:     fmt.Sprintf("%d GB", hp.GetTotalStorageGB()),
		LastSync: now,
	}

	if ssd := hp.GetSSDStorageGB(); ssd > 0 {
		result["storage-ssd"] = state.HardwareState{
			Type:     "storage-ssd",
			Size:     fmt.Sprintf("%d GB", ssd),
			LastSync: now,
		}
	}

	if hdd := hp.GetHDDStorageGB(); hdd > 0 {
		result["storage-hdd"] = state.HardwareState{
			Type:     "storage-hdd",
			Size:     fmt.Sprintf("%d GB", hdd),
			LastSync: now,
		}
	}

	logx.As().Debug().Any("result", result).Msg("Refreshed hardware state")
	return result
}
