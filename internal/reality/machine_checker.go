// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/hardware"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"github.com/joomcode/errorx"
	htime "helm.sh/helm/v3/pkg/time"
)

// machineChecker probes the local host: software binaries and hardware metrics.
// It depends only on a state.Manager (to read the latest persisted software state)
// and optional path overrides for sandbox bin / state directories.
type machineChecker struct {
	Base
	sandboxBinDir      string
	stateDir           string
	softwareInstallers map[string]software.Software
}

// NewMachineChecker constructs a machineChecker.
// sandboxBinDir and stateDir may be empty strings; the checker will fall back
// to models.Paths() values at call time.
func NewMachineChecker(sm state.Manager, sandboxBinDir, stateDir string) (Checker[state.MachineState], error) {
	softwareInstallers := make(map[string]software.Software)
	for name, installerFunc := range software.Installers() {
		inst, err := installerFunc()
		if err != nil {
			return nil, errorx.IllegalState.Wrap(err, "failed to create installer for %q", name)
		}
		softwareInstallers[name] = inst
	}

	return &machineChecker{
		Base:               Base{sm: sm},
		sandboxBinDir:      sandboxBinDir,
		stateDir:           stateDir,
		softwareInstallers: softwareInstallers,
	}, nil
}

// FlushState first refreshes the state from disk to get the latest MachineState,
// then compares the existing MachineState with the new one. If they are equal,
// no write is performed. If they differ, the new MachineState is persisted to disk.
// This pattern of refreshing before writing is necessary to prevent overwriting
// concurrent updates to other parts of the state by separate reality checkers.
func (m *machineChecker) FlushState(st state.MachineState) error {
	if err := m.sm.Refresh(); err != nil && !errorx.IsOfType(err, state.NotFoundError) {
		return ErrFlushError.Wrap(err, "failed to refresh state")
	}

	existing := m.sm.State().MachineState
	if existing.Equal(st) {
		return nil
	}

	fullState := m.sm.State()
	fullState.MachineState = st
	if err := m.sm.Set(fullState).FlushState(); err != nil {
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

	ms := current.MachineState
	ms.Software = m.refreshSoftwareState()
	ms.Hardware = m.refreshHardwareState()
	ms.LastSync = htime.Now()

	if err := m.FlushState(ms); err != nil {
		return ms, err
	}

	return ms, nil
}

// refreshSoftwareState checks the presence and versions of relevant software binaries on the host.
func (m *machineChecker) refreshSoftwareState() map[string]state.SoftwareState {
	result := make(map[string]state.SoftwareState)
	for name, inst := range m.softwareInstallers {
		st, err := inst.VerifyInstallation()
		if err != nil {
			logx.As().Error().Err(err).Str("software", name).Msg("Error verifying software installation")
			continue
		}

		result[name] = *st
	}

	logx.As().Debug().Any("result", result).Msg("Refreshed software state")

	return result
}

// refreshHardwareState collects current host hardware metrics.
func (m *machineChecker) refreshHardwareState() map[string]state.HardwareState {
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
