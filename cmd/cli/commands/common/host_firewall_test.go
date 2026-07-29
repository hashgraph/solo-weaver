// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
)

// TestApplyPersistedFirewallContent_ConfigWins verifies the config > state
// precedence (issue #932, AC4): fields the operator supplied via --config are
// left untouched, and only the fields config left empty are filled from the
// persisted firewall state. This is the core of SeedHostFirewallFromState /
// mergeHostFirewallFromState, so a regression here would let persisted state
// silently override an explicit --config value.
func TestApplyPersistedFirewallContent_ConfigWins(t *testing.T) {
	persisted := &models.HostConfig{
		ManagementCIDRs: []string{"10.0.0.0/8"},
		BlockedCIDRs:    []string{"192.0.2.0/24"},
		SSHPort:         2222,
		PodCIDR:         "10.4.0.0/14",
		InClusterPorts:  []int{8080},
		Disabled:        true, // decision must NOT be applied by this helper
	}

	cfg := models.HostConfig{
		// Operator supplied a management allowlist and SSH port via --config;
		// everything else is left to fall back to state.
		ManagementCIDRs: []string{"203.0.113.0/24"},
		SSHPort:         22,
	}

	got := applyPersistedFirewallContent(cfg, persisted)

	assert.Equal(t, []string{"203.0.113.0/24"}, got.ManagementCIDRs, "config allowlist must win over state")
	assert.Equal(t, 22, got.SSHPort, "config SSH port must win over state")
	assert.Equal(t, []string{"192.0.2.0/24"}, got.BlockedCIDRs, "empty blocked CIDRs must fall back to state")
	assert.Equal(t, "10.4.0.0/14", got.PodCIDR, "empty pod CIDR must fall back to state")
	assert.Equal(t, []int{8080}, got.InClusterPorts, "empty in-cluster ports must fall back to state")
	assert.False(t, got.Disabled, "the enable/disable decision must not be touched by the content merge")
}

// TestApplyPersistedFirewallContent_NilPersistedIsNoOp verifies that a nil
// persisted firewall (fresh host, nothing ever recorded) leaves config untouched.
func TestApplyPersistedFirewallContent_NilPersistedIsNoOp(t *testing.T) {
	cfg := models.HostConfig{ManagementCIDRs: []string{"203.0.113.0/24"}}
	got := applyPersistedFirewallContent(cfg, nil)
	assert.Equal(t, cfg, got)
}
