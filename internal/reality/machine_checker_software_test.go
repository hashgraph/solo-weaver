// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// The provisioner under test has moved its catalog default to 1.36.5 while the
// host still runs the 1.33.4 binaries an older provisioner installed.
const (
	installedK8s = "1.33.4"
	defaultK8s   = "1.36.5"
)

// stubCatalog makes isCatalogVersion accept the listed versions and
// identifyInstalledVersions report ids per artifact. It returns a pointer to the
// number of identification calls, i.e. how often binaries were hashed.
func stubCatalog(t *testing.T, known []string, ids map[string][]string, idErr error) *int {
	t.Helper()
	calls := 0
	origKnown, origID := isCatalogVersion, identifyInstalledVersions
	isCatalogVersion = func(_ string, version string) (bool, error) {
		for _, v := range known {
			if v == version {
				return true, nil
			}
		}
		return false, nil
	}
	identifyInstalledVersions = func(artifact string) ([]string, error) {
		calls++
		return ids[artifact], idErr
	}
	t.Cleanup(func() { isCatalogVersion, identifyInstalledVersions = origKnown, origID })
	return &calls
}

// installerReporting returns an installer whose VerifyInstallation reports what
// the real one does: the catalog default as the version, and whether the
// binaries are present.
func installerReporting(t *testing.T, name, catalogDefault string, installed bool) software.Software {
	t.Helper()
	inst := software.NewMockSoftware(gomock.NewController(t))
	inst.EXPECT().VerifyInstallation().DoAndReturn(func() (*state.SoftwareState, error) {
		return &state.SoftwareState{Name: name, Version: catalogDefault, Installed: installed}, nil
	}).AnyTimes()
	return inst
}

func checkerWith(sm state.Manager, installers map[string]software.Software) *machineChecker {
	return &machineChecker{sm: sm, softwareInstallers: installers}
}

func persistedSoftware(entries map[string]state.SoftwareState) state.State {
	s := state.State{}
	s.MachineState = state.NewMachineState()
	for k, v := range entries {
		s.MachineState.Software[k] = v
	}
	return s
}

func refreshSoftware(t *testing.T, c *machineChecker) map[string]state.SoftwareState {
	t.Helper()
	ms, err := c.RefreshState(context.Background())
	require.NoError(t, err)
	return ms.Software
}

func TestRefreshSoftware_KeepsValidRecordedVersionWithoutHashing(t *testing.T) {
	calls := stubCatalog(t, []string{installedK8s, defaultK8s}, nil, nil)
	persisted := persistedSoftware(map[string]state.SoftwareState{
		"kubelet": {Name: "kubelet", Version: installedK8s, Installed: true},
	})

	got := refreshSoftware(t, checkerWith(fakeMachineStateManager{st: persisted}, map[string]software.Software{
		"kubelet": installerReporting(t, "kubelet", defaultK8s, true),
	}))

	assert.Equal(t, installedK8s, got["kubelet"].Version, "the recorded version must survive a newer catalog default")
	assert.True(t, got["kubelet"].Installed)
	assert.Zero(t, *calls, "a valid record must not trigger hashing")
}

func TestRefreshSoftware_IdentifiesUnrecordedBinaryByChecksum(t *testing.T) {
	stubCatalog(t, []string{installedK8s, defaultK8s}, map[string][]string{"kubeadm": {installedK8s}}, nil)

	got := refreshSoftware(t, checkerWith(fakeMachineStateManager{st: persistedSoftware(nil)}, map[string]software.Software{
		"kubeadm": installerReporting(t, "kubeadm", defaultK8s, true),
	}))

	assert.Equal(t, installedK8s, got["kubeadm"].Version, "an unrecorded binary must be identified, never assumed to be the default")
}

func TestRefreshSoftware_StaleNotInstalledRecordIsNotAVersion(t *testing.T) {
	// A not-installed entry carries the default of the provisioner that wrote it.
	// Once the binary appears, its version comes from its checksum.
	calls := stubCatalog(t, []string{"18.6.4", "18.11.3"}, map[string][]string{"teleport": {"18.11.3"}}, nil)
	persisted := persistedSoftware(map[string]state.SoftwareState{
		"teleport": {Name: "teleport", Version: "18.6.4", Installed: false},
	})

	got := refreshSoftware(t, checkerWith(fakeMachineStateManager{st: persisted}, map[string]software.Software{
		"teleport": installerReporting(t, "teleport", "18.11.3", true),
	}))

	assert.Equal(t, "18.11.3", got["teleport"].Version)
	assert.Equal(t, 1, *calls)
}

func TestRefreshSoftware_FreshInstallRecordsDefault(t *testing.T) {
	stubCatalog(t, []string{installedK8s, defaultK8s}, map[string][]string{"kubelet": {defaultK8s}}, nil)

	got := refreshSoftware(t, checkerWith(fakeMachineStateManager{st: persistedSoftware(nil)}, map[string]software.Software{
		"kubelet": installerReporting(t, "kubelet", defaultK8s, true),
	}))

	assert.Equal(t, defaultK8s, got["kubelet"].Version)
}

func TestRefreshSoftware_DelistedRecord(t *testing.T) {
	persisted := persistedSoftware(map[string]state.SoftwareState{
		"kubectl": {Name: "kubectl", Version: "1.30.0", Installed: true},
	})
	installers := map[string]software.Software{"kubectl": installerReporting(t, "kubectl", defaultK8s, true)}

	t.Run("identified", func(t *testing.T) {
		stubCatalog(t, []string{installedK8s, defaultK8s}, map[string][]string{"kubectl": {installedK8s}}, nil)
		got := refreshSoftware(t, checkerWith(fakeMachineStateManager{st: persisted}, installers))
		assert.Equal(t, installedK8s, got["kubectl"].Version)
	})

	t.Run("unidentified keeps the old record", func(t *testing.T) {
		stubCatalog(t, []string{installedK8s, defaultK8s}, nil, nil)
		got := refreshSoftware(t, checkerWith(fakeMachineStateManager{st: persisted}, installers))
		assert.Equal(t, "1.30.0", got["kubectl"].Version, "with no evidence, keep the record rather than guess")
	})
}

func TestRefreshSoftware_UnknownOrAmbiguousBinaryRecordsNoVersion(t *testing.T) {
	cases := map[string]struct {
		ids   []string
		idErr error
	}{
		"unknown":   {ids: nil},
		"ambiguous": {ids: []string{installedK8s, defaultK8s}},
		"error":     {idErr: assert.AnError},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			stubCatalog(t, []string{installedK8s, defaultK8s}, map[string][]string{"kubelet": tc.ids}, tc.idErr)

			got := refreshSoftware(t, checkerWith(fakeMachineStateManager{st: persistedSoftware(nil)}, map[string]software.Software{
				"kubelet": installerReporting(t, "kubelet", defaultK8s, true),
			}))

			assert.Empty(t, got["kubelet"].Version, "never record the default without a unique checksum match")
			assert.True(t, got["kubelet"].Installed, "an unknown binary must not fail the refresh or vanish")
		})
	}
}

func TestRefreshSoftware_RecordedMismatchIsNotRewritten(t *testing.T) {
	// Recorded 1.33.4 is valid, so the checker does not hash. If the bytes have
	// changed, VerifyExecutables reports it at time of use instead of the record
	// quietly following the bytes.
	calls := stubCatalog(t, []string{installedK8s, defaultK8s}, map[string][]string{"kubeadm": {defaultK8s}}, nil)
	persisted := persistedSoftware(map[string]state.SoftwareState{
		"kubeadm": {Name: "kubeadm", Version: installedK8s, Installed: true},
	})

	got := refreshSoftware(t, checkerWith(fakeMachineStateManager{st: persisted}, map[string]software.Software{
		"kubeadm": installerReporting(t, "kubeadm", defaultK8s, true),
	}))

	assert.Equal(t, installedK8s, got["kubeadm"].Version)
	assert.Zero(t, *calls)
}

func TestRefreshSoftware_MissingBinaryIsNotInstalled(t *testing.T) {
	calls := stubCatalog(t, []string{installedK8s, defaultK8s}, nil, nil)
	persisted := persistedSoftware(map[string]state.SoftwareState{
		"kubelet": {Name: "kubelet", Version: installedK8s, Installed: true},
	})

	got := refreshSoftware(t, checkerWith(fakeMachineStateManager{st: persisted}, map[string]software.Software{
		"kubelet": installerReporting(t, "kubelet", defaultK8s, false),
	}))

	assert.False(t, got["kubelet"].Installed, "a missing binary must not be recorded as installed")
	assert.Zero(t, *calls, "nothing to hash when the binary is absent")
}

func TestRefreshSoftware_FindsRecordKeyedByInstallerName(t *testing.T) {
	calls := stubCatalog(t, []string{installedK8s, defaultK8s}, nil, nil)
	persisted := persistedSoftware(map[string]state.SoftwareState{
		"legacy-key": {Name: "cri-o", Version: installedK8s, Installed: true},
	})

	got := refreshSoftware(t, checkerWith(fakeMachineStateManager{st: persisted}, map[string]software.Software{
		"crio": installerReporting(t, "cri-o", defaultK8s, true),
	}))

	assert.Equal(t, installedK8s, got["crio"].Version)
	assert.Zero(t, *calls)
}

// TestRefreshSoftware_RepeatedFlushesKeepInstalledVersions follows what every
// BLL command does at the end — refresh reality, then persist the whole state —
// several times against a real state file.
func TestRefreshSoftware_RepeatedFlushesKeepInstalledVersions(t *testing.T) {
	calls := stubCatalog(t, []string{installedK8s, defaultK8s}, nil, nil)

	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)
	stateFile := filepath.Join(t.TempDir(), state.StateFileName)

	seed := state.NewState(stateFile)
	for _, name := range []string{"kubelet", "kubeadm", "kubectl"} {
		seed.MachineState.Software[name] = state.SoftwareState{Name: name, Version: installedK8s, Installed: true}
	}
	writer, err := state.NewStateManager(state.WithState(seed), state.WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, writer.FlushAll())

	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(fm))
	require.NoError(t, err)
	checker := checkerWith(sm, map[string]software.Software{
		"kubelet": installerReporting(t, "kubelet", defaultK8s, true),
		"kubeadm": installerReporting(t, "kubeadm", defaultK8s, true),
		"kubectl": installerReporting(t, "kubectl", defaultK8s, true),
	})

	for i := 0; i < 3; i++ {
		ms, err := checker.RefreshState(context.Background())
		require.NoError(t, err)
		s := sm.State()
		s.MachineState = ms
		require.NoError(t, sm.Set(s).FlushAll())
	}

	reader, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(fm))
	require.NoError(t, err)
	require.NoError(t, reader.Refresh())
	for _, name := range []string{"kubelet", "kubeadm", "kubectl"} {
		assert.Equal(t, installedK8s, reader.State().MachineState.Software[name].Version, "%s version after repeated flushes", name)
	}
	assert.Zero(t, *calls, "valid records must not be re-hashed on every command")
}
