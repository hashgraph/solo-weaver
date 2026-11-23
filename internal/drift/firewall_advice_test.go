// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package drift

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/stretchr/testify/require"
)

const wantFirewallAdvice = "firewall: to repair these, see docs/commands/network/firewall.md, " +
	"\"Changes made outside solo-provisioner are reported\". " +
	"Every `solo-provisioner network firewall` command that applies a table replaces all of it, " +
	"as can a reboot or this run, so an entry only in the live table, such as a management address " +
	"added by hand, is lost unless it is first added to the config"

// One line holds for every kind of change in every command: what repairs a
// change depends on the host and on what the run itself did to the table.
func TestFirewallAdvice_OneLineForAnyFirewallChange(t *testing.T) {
	for name, tc := range map[string]struct {
		record   *state.HostFirewallState
		observed state.HostFirewallObservation
	}{
		"table live on a host recorded as disabled": {disabled, state.HostFirewallObservation{TablePresent: true, ConfigPresent: true}},
		"config without a live table":               {enabled, state.HostFirewallObservation{ConfigPresent: true}},
		"live table without a config":               {enabled, state.HostFirewallObservation{TablePresent: true}},
		"content differs from the config": {enabled, state.HostFirewallObservation{TablePresent: true, ConfigPresent: true, Fields: []state.HostFirewallFieldObservation{
			{Field: "configFile.mgmt.cidrs", Config: []string{"10.0.0.0/8"}, Live: []string{"10.0.0.0/8", "203.0.113.7/32"}},
			{Field: "configFile.mgmt.ports", Config: []string{"22"}, Live: []string{"22-23"}},
		}, Rules: []state.HostFirewallRuleObservation{{Name: "admin", InConfig: true}}}},
		"table not read":        {enabled, state.HostFirewallObservation{Unobserved: true, ConfigPresent: true}},
		"config does not parse": {enabled, state.HostFirewallObservation{TablePresent: true, ConfigPresent: true, ConfigUnreadable: true}},
	} {
		t.Run(name, func(t *testing.T) {
			changes := Firewall(firewallStates(tc.record, &tc.observed))
			require.NotEmpty(t, changes)

			require.Equal(t, []string{wantFirewallAdvice}, FirewallAdvice(changes))
		})
	}
}

func TestFirewallAdvice_NothingForNoFirewallChange(t *testing.T) {
	require.Empty(t, FirewallAdvice(nil))
	require.Empty(t, FirewallAdvice([]Change{
		{Component: "teleport", Field: "chartVersion", Persisted: "1", Live: "2"},
	}))
}

func TestFirewallAdvice_NamesASectionThatExists(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "commands", "network", "firewall.md"))
	require.NoError(t, err)

	require.True(t, strings.Contains(string(doc), "\n### Changes made outside solo-provisioner are reported\n"))
}
