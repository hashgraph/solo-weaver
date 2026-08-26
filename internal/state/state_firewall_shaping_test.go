// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package state

import (
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"gopkg.in/yaml.v3"
)

// TestFirewallAndShaping_RoundTrip verifies the new MachineState.Firewall and
// BlockNodeState.Shaping fields marshal to the expected YAML keys and unmarshal
// back losslessly, locking in the yaml tags on HostFirewallState / ShapingState.
func TestFirewallAndShaping_RoundTrip(t *testing.T) {
	prio := 1
	original := State{
		StateRecord: StateRecord{
			MachineState: MachineState{
				Firewall: &HostFirewallState{
					Disabled:        false,
					ManagementCIDRs: []string{"10.0.0.0/8"},
					BlockedCIDRs:    []string{"192.0.2.0/24"},
					MgmtPorts:       []int{2222},
					PodCIDR:         "10.4.0.0/14",
					InClusterPorts:  []int{8080, 9090},
				},
			},
			BlockNodeState: BlockNodeState{
				Shaping: &ShapingState{
					EgressInterface: "eth0",
					LinkRate:        "1gbit",
					ShapeOverrides: map[string]models.ShapeOverride{
						"publisher": {Rate: "800mbit", Ceil: "1gbit", Prio: &prio},
					},
				},
			},
		},
	}

	out, err := yaml.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Spot-check the on-disk key names (the operator/other tools read these).
	for _, key := range []string{"managementCidrs", "mgmtPorts", "podCidr", "egressInterface", "linkRate", "shapeOverrides"} {
		if !strings.Contains(string(out), key) {
			t.Errorf("expected marshalled YAML to contain key %q\n%s", key, out)
		}
	}

	var got State
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	fw := got.MachineState.Firewall
	if fw == nil {
		t.Fatal("Firewall lost in round-trip")
	}
	if len(fw.MgmtPorts) != 1 || fw.MgmtPorts[0] != 2222 || fw.PodCIDR != "10.4.0.0/14" ||
		len(fw.ManagementCIDRs) != 1 || len(fw.InClusterPorts) != 2 {
		t.Errorf("Firewall not preserved in round-trip: %+v", fw)
	}

	sh := got.BlockNodeState.Shaping
	if sh == nil {
		t.Fatal("Shaping lost in round-trip")
	}
	if sh.EgressInterface != "eth0" || sh.LinkRate != "1gbit" {
		t.Errorf("Shaping NIC/rate not preserved: %+v", sh)
	}
	ov, ok := sh.ShapeOverrides["publisher"]
	if !ok || ov.Rate != "800mbit" || ov.Ceil != "1gbit" || ov.Prio == nil || *ov.Prio != 1 {
		t.Errorf("Shaping override not preserved: %+v", sh.ShapeOverrides)
	}
}

// TestFirewallAndShaping_OldFileLoads verifies a state file written before these
// fields existed unmarshals cleanly with nil Firewall/Shaping (AC6).
func TestFirewallAndShaping_OldFileLoads(t *testing.T) {
	old := []byte(`
state:
  version: v2
  machineState:
    profile: mainnet
  blockNodeState:
    name: block-node
    trafficShapingDisabled: true
`)
	var got State
	if err := yaml.Unmarshal(old, &got); err != nil {
		t.Fatalf("old state file failed to load: %v", err)
	}
	if got.MachineState.Firewall != nil {
		t.Errorf("expected nil Firewall for old file, got %+v", got.MachineState.Firewall)
	}
	if got.BlockNodeState.Shaping != nil {
		t.Errorf("expected nil Shaping for old file, got %+v", got.BlockNodeState.Shaping)
	}
	if !got.BlockNodeState.TrafficShapingDisabled {
		t.Error("expected existing TrafficShapingDisabled to still parse as true")
	}
}
