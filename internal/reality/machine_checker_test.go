// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"context"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
)

// fakeMachineStateManager satisfies state.Manager by embedding the interface
// (nil) and overriding only the two methods machineChecker.RefreshState calls:
// Refresh() and State(). Any other call would panic, keeping the fake honest.
type fakeMachineStateManager struct {
	state.Manager
	st state.State
}

func (f fakeMachineStateManager) Refresh() error     { return nil }
func (f fakeMachineStateManager) State() state.State { return f.st }

// TestMachineRefreshState_PreservesFirewall verifies that the persisted
// host-firewall configuration (host-scoped, not observable from the cluster)
// survives a reality refresh. Unlike the block-node checker, the machine checker
// starts from the current MachineState and overwrites only Software/Hardware/
// LastSync, so Firewall is auto-preserved — this test locks that in so a future
// refactor that rebuilds MachineState from scratch does not silently drop it.
func TestMachineRefreshState_PreservesFirewall(t *testing.T) {
	persisted := state.NewMachineState()
	persisted.Profile = "mainnet"
	persisted.Firewall = &state.HostFirewallState{
		ManagementCIDRs: []string{"10.0.0.0/8"},
		BlockedCIDRs:    []string{"192.0.2.0/24"},
		SSHPort:         2222,
		PodCIDR:         "10.4.0.0/14",
		InClusterPorts:  []int{8080},
	}

	full := state.State{}
	full.MachineState = persisted

	// softwareInstallers is left nil: refreshSoftwareState ranges over it and
	// returns an empty map, which is all this test needs.
	checker := &machineChecker{sm: fakeMachineStateManager{st: full}}

	got, err := checker.RefreshState(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Profile != "mainnet" {
		t.Errorf("Profile must be preserved, got %q", got.Profile)
	}
	if got.Firewall == nil {
		t.Fatal("Firewall must be preserved across a reality refresh, got nil")
	}
	if len(got.Firewall.ManagementCIDRs) != 1 || got.Firewall.ManagementCIDRs[0] != "10.0.0.0/8" {
		t.Errorf("Firewall management CIDRs not preserved: %+v", got.Firewall.ManagementCIDRs)
	}
	if got.Firewall.SSHPort != 2222 {
		t.Errorf("Firewall SSH port not preserved: got %d, want 2222", got.Firewall.SSHPort)
	}
}
