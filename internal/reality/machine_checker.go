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
	sm                 state.Manager
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
		sm:                 sm,
		sandboxBinDir:      sandboxBinDir,
		stateDir:           stateDir,
		softwareInstallers: softwareInstallers,
	}, nil
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
	ms.Software = m.refreshSoftwareState(current.MachineState.Software)
	ms.Hardware = m.refreshHardwareState()
	ms.LastSync = htime.Now()

	return ms, nil
}

// identifyInstalledVersions and isCatalogVersion are package vars so tests can
// stub them without real binaries or the embedded catalog.
var (
	identifyInstalledVersions = software.IdentifyInstalledVersions
	isCatalogVersion          = software.IsCatalogVersion
)

// refreshSoftwareState checks the presence and versions of relevant software binaries on the host.
// persisted is the previously recorded software state, keyed like the result.
func (m *machineChecker) refreshSoftwareState(persisted map[string]state.SoftwareState) map[string]state.SoftwareState {
	result := make(map[string]state.SoftwareState)
	for name, inst := range m.softwareInstallers {
		st, err := inst.VerifyInstallation()
		if err != nil {
			logx.As().Error().Err(err).Str("software", name).Msg("Error verifying software installation")
			continue
		}

		if st.Installed {
			prev, found := recordedSoftware(persisted, name, st.Name)
			st.Version = installedVersion(st.Name, prev, found)
		}

		result[name] = *st
	}

	logx.As().Debug().Any("result", result).Msg("Refreshed software state")

	return result
}

// installedVersion returns the version to record for an installed artifact.
// VerifyInstallation reports the running provisioner's catalog default, which is
// wrong for a binary an older provisioner installed. So a valid recorded version
// is kept without hashing, and anything else is identified by checksum. An empty
// result means the version is unknown; VerifyExecutables then refuses to run the
// binary, so only commands that need it fail.
func installedVersion(artifact string, prev state.SoftwareState, found bool) string {
	recorded := ""
	if found && prev.Installed {
		recorded = prev.Version
	}

	if recorded != "" {
		ok, err := isCatalogVersion(artifact, recorded)
		if err == nil && ok {
			return recorded
		}
	}

	matches, err := identifyInstalledVersions(artifact)
	switch {
	case err != nil:
		logx.As().Warn().Err(err).Str("software", artifact).Str("recordedVersion", recorded).
			Msg("Could not identify the installed version by checksum")
	case len(matches) == 1:
		logx.As().Info().Str("software", artifact).Str("version", matches[0]).
			Msg("Identified the installed version by checksum")
		return matches[0]
	case len(matches) == 0:
		logx.As().Warn().Str("software", artifact).Str("recordedVersion", recorded).
			Msg("Installed binaries match no version in the infrastructure catalog")
	default:
		logx.As().Warn().Str("software", artifact).Strs("matchingVersions", matches).
			Msg("Installed binaries match several catalog versions; not recording a version")
	}

	// Keep a record the catalog no longer lists rather than guess a version.
	return recorded
}

// recordedSoftware finds a component's persisted entry by its result key, or by
// artifact name for entries keyed differently (e.g. "crio" vs "cri-o").
func recordedSoftware(persisted map[string]state.SoftwareState, key, artifact string) (state.SoftwareState, bool) {
	if prev, ok := persisted[key]; ok {
		return prev, true
	}
	for _, prev := range persisted {
		if prev.Name == artifact {
			return prev, true
		}
	}
	return state.SoftwareState{}, false
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
