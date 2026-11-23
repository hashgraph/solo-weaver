// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package drift

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/automa-saga/logx"
	"github.com/rs/zerolog"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/stretchr/testify/require"
)

// firewallStates returns a baseline holding record (nil for no record) and a
// live state holding observed.
func firewallStates(record *state.HostFirewallState, observed *state.HostFirewallObservation) (state.State, state.State) {
	var baseline, live state.State
	baseline.MachineState.Firewall = record
	live.MachineState.Firewall = record
	live.MachineState.FirewallObserved = observed
	return baseline, live
}

var (
	enabled  = &state.HostFirewallState{}
	disabled = &state.HostFirewallState{Disabled: true}
)

func TestFirewall_NothingReadIsSilent(t *testing.T) {
	require.Empty(t, Firewall(firewallStates(enabled, nil)))
	require.Empty(t, Firewall(firewallStates(disabled, nil)))
}

func TestFirewall_AgreeingHostHasNoChanges(t *testing.T) {
	for name, tc := range map[string]struct {
		record   *state.HostFirewallState
		observed state.HostFirewallObservation
	}{
		"enabled, config and table":       {enabled, state.HostFirewallObservation{ConfigPresent: true, TablePresent: true}},
		"enabled, no config, no table":    {enabled, state.HostFirewallObservation{}},
		"disabled, no table":              {disabled, state.HostFirewallObservation{}},
		"disabled, config kept, no table": {disabled, state.HostFirewallObservation{ConfigPresent: true}},
		"no record, config and table":     {nil, state.HostFirewallObservation{ConfigPresent: true, TablePresent: true}},
		"no record, nothing on the host":  {nil, state.HostFirewallObservation{}},
	} {
		t.Run(name, func(t *testing.T) {
			observed := tc.observed
			require.Empty(t, Firewall(firewallStates(tc.record, &observed)))
		})
	}
}

func TestFirewall_DisabledButTableLive(t *testing.T) {
	got := Firewall(firewallStates(disabled, &state.HostFirewallObservation{ConfigPresent: true, TablePresent: true}))

	require.Equal(t, []Change{
		{Component: "firewall", Field: "disabled", Persisted: "true", Live: "false"},
	}, got)
}

func TestFirewall_ConfigButNoTableIsARemoval(t *testing.T) {
	for name, record := range map[string]*state.HostFirewallState{"enabled": enabled, "no record": nil} {
		t.Run(name, func(t *testing.T) {
			got := Firewall(firewallStates(record, &state.HostFirewallObservation{ConfigPresent: true}))

			require.Equal(t, []Change{
				{Component: "firewall", Field: "configFile", Persisted: "present", Live: "absent", Removed: true},
			}, got)
		})
	}
}

func TestFirewall_TableWithoutAConfig(t *testing.T) {
	got := Firewall(firewallStates(enabled, &state.HostFirewallObservation{TablePresent: true}))

	require.Equal(t, []Change{
		{Component: "firewall", Field: "configFile", Persisted: "absent", Live: "present"},
	}, got)
}

// withFields is an enabled host whose config and table both exist, holding fields.
func withFields(fields ...state.HostFirewallFieldObservation) *state.HostFirewallObservation {
	return &state.HostFirewallObservation{TablePresent: true, ConfigPresent: true, Fields: fields}
}

func TestFirewall_EqualFieldsHaveNoChanges(t *testing.T) {
	observed := withFields(state.HostFirewallFieldObservation{
		Field: "configFile.mgmt.cidrs", Config: []string{"10.0.0.0/8"}, Live: []string{"10.0.0.0/8"},
	})

	require.Empty(t, Firewall(firewallStates(enabled, observed)))
}

func TestFirewall_DifferingFieldShowsEachSidesOwnItems(t *testing.T) {
	observed := withFields(
		state.HostFirewallFieldObservation{
			Field:  "configFile.mgmt.cidrs",
			Config: []string{"10.0.0.0/8", "192.168.0.0/16", "198.51.100.9/32"},
			Live:   []string{"10.0.0.0/8", "192.168.0.0/16", "203.0.113.7/32"},
		},
		state.HostFirewallFieldObservation{
			Field: "configFile.mgmt.ports", Config: []string{"22"}, Live: []string{"22-23"},
		},
		state.HostFirewallFieldObservation{
			Field: "configFile.blocked.cidrs", Config: []string{"203.0.113.0/24"}, Live: nil,
		},
	)

	require.Equal(t, []Change{
		{Component: "firewall", Field: "configFile.mgmt.cidrs", Persisted: "198.51.100.9/32, 2 in both", Live: "203.0.113.7/32, 2 in both"},
		{Component: "firewall", Field: "configFile.mgmt.ports", Persisted: "22", Live: "22-23"},
		{Component: "firewall", Field: "configFile.blocked.cidrs", Persisted: "203.0.113.0/24", Live: "none"},
	}, Firewall(firewallStates(enabled, observed)))
}

func TestFirewall_DisabledRecordSkipsContent(t *testing.T) {
	var logs bytes.Buffer
	prevLogger, prevLevel := *logx.As(), zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	logx.SetLogger(zerolog.New(&logs))
	t.Cleanup(func() {
		logx.SetLogger(prevLogger)
		zerolog.SetGlobalLevel(prevLevel)
	})

	observed := withFields(state.HostFirewallFieldObservation{
		Field: "configFile.mgmt.cidrs", Config: []string{"10.0.0.0/8"}, Live: []string{"203.0.113.7/32"},
	})

	require.Equal(t, []Change{
		{Component: "firewall", Field: "disabled", Persisted: "true", Live: "false"},
	}, Firewall(firewallStates(disabled, observed)))
	// The reader logged the content as read; this says why no line follows.
	require.Contains(t, logs.String(), "host firewall content not compared: recorded as disabled")
}

func field(name string, config, live []string) state.HostFirewallFieldObservation {
	return state.HostFirewallFieldObservation{Field: name, Config: config, Live: live}
}

func TestFirewall_AllowRulePresenceAndOrder(t *testing.T) {
	observed := withFields(
		field("configFile.mgmt.cidrs", []string{"10.0.0.0/8"}, []string{"10.0.0.0/8", "203.0.113.7/32"}),
		field("configFile.allow[k8s-node].cidrs", []string{"10.0.0.0/24"}, []string{"10.0.0.0/24"}),
		field("configFile.allow[k8s-node].ports", []string{"6443"}, []string{"6443", "8080"}),
	)
	observed.Rules = []state.HostFirewallRuleObservation{
		{Name: "admin", InConfig: true, Live: false},
		{Name: "k8s-node", InConfig: true, Live: true},
		{Name: "stray", InConfig: false, Live: true},
	}

	require.Equal(t, []Change{
		{Component: "firewall", Field: "configFile.mgmt.cidrs", Persisted: "1 in both", Live: "203.0.113.7/32, 1 in both"},
		{Component: "firewall", Field: "configFile.allow[admin]", Persisted: "present", Live: "absent", Removed: true},
		{Component: "firewall", Field: "configFile.allow[k8s-node].ports", Persisted: "1 in both", Live: "8080, 1 in both"},
		{Component: "firewall", Field: "configFile.allow[stray]", Persisted: "absent", Live: "present"},
	}, Firewall(firewallStates(enabled, observed)))
}

func TestFirewall_LongSidesAreCapped(t *testing.T) {
	live := []string{"10.0.0.0/8"}
	for _, a := range []string{"1", "2", "3", "4", "5", "6", "7"} {
		live = append(live, "198.51.100."+a+"/32")
	}
	observed := withFields(field("configFile.mgmt.cidrs", []string{"10.0.0.0/8"}, live))

	got := Firewall(firewallStates(enabled, observed))

	require.Equal(t, "198.51.100.1/32, 198.51.100.2/32, 198.51.100.3/32, 198.51.100.4/32, 198.51.100.5/32, ... and 2 more, 1 in both", got[0].Live)
}

func TestFirewall_LongItemsAreCappedByLength(t *testing.T) {
	// Five IPv6 prefixes of 43 bytes pass 200 bytes before the fifth.
	var live []string
	for _, a := range []string{"1", "2", "3", "4", "5"} {
		live = append(live, "2001:db8:ffff:ffff:ffff:ffff:ffff:fff"+a+"/128")
	}
	observed := withFields(field("configFile.mgmt.cidrs", nil, live))

	got := Firewall(firewallStates(enabled, observed))

	require.Equal(t, "2001:db8:ffff:ffff:ffff:ffff:ffff:fff1/128, 2001:db8:ffff:ffff:ffff:ffff:ffff:fff2/128, "+
		"2001:db8:ffff:ffff:ffff:ffff:ffff:fff3/128, 2001:db8:ffff:ffff:ffff:ffff:ffff:fff4/128, ... and 1 more", got[0].Live)
}

func TestFirewall_AtMostTenObjectChangesThenOneCatchAll(t *testing.T) {
	observed := withFields()
	for i := range 13 {
		name := "r" + string(rune('a'+i))
		observed.Rules = append(observed.Rules, state.HostFirewallRuleObservation{Name: name, Live: true})
	}

	got := Firewall(firewallStates(enabled, observed))

	require.Len(t, got, 11)
	require.Equal(t, "configFile.allow[rj]", got[9].Field)
	require.Equal(t, Change{
		Component: "firewall", Field: "configFile.other",
		Persisted: "configFile.allow[rk], configFile.allow[rl], configFile.allow[rm]",
		Live:      "see the debug log",
	}, got[10])
}

func TestFirewall_UnreadHostWithoutAConfigIsSilent(t *testing.T) {
	for _, record := range []*state.HostFirewallState{enabled, disabled, nil} {
		require.Empty(t, Firewall(firewallStates(record, &state.HostFirewallObservation{Unobserved: true})))
	}
}

func TestFirewall_UnreadHostWithAConfigIsReportedOnceOnItsPresenceField(t *testing.T) {
	unread := &state.HostFirewallObservation{Unobserved: true, ConfigPresent: true}

	require.Equal(t, []Change{
		{Component: "firewall", Field: "configFile", Persisted: "present", Unobserved: true},
	}, Firewall(firewallStates(enabled, unread)))
	require.Equal(t, []Change{
		{Component: "firewall", Field: "configFile", Persisted: "present", Unobserved: true},
	}, Firewall(firewallStates(nil, unread)))
	require.Equal(t, []Change{
		{Component: "firewall", Field: "disabled", Persisted: "true", Unobserved: true},
	}, Firewall(firewallStates(disabled, unread)))
}

func TestFirewall_UnreadableConfigOverALiveTable(t *testing.T) {
	observed := &state.HostFirewallObservation{TablePresent: true, ConfigPresent: true, ConfigUnreadable: true}

	require.Equal(t, []Change{
		{Component: "firewall", Field: "configFile", Persisted: "present", Unobserved: true},
	}, Firewall(firewallStates(enabled, observed)))
	// A disabled record still gets its own line: the table was read.
	require.Equal(t, []Change{
		{Component: "firewall", Field: "disabled", Persisted: "true", Live: "false"},
	}, Firewall(firewallStates(disabled, observed)))
}

// firewallRecordCompared lists the HostFirewallState keys drift.Firewall
// compares: only the decision. firewallRecordNotCompared lists the rest, with
// the reason.
var (
	firewallRecordCompared = map[string]bool{"disabled": true}

	firewallRecordNotCompared = map[string]string{
		"managementCidrs": "compared from the firewall config file: network firewall verbs record only the decision",
		"blockedCidrs":    "compared from the firewall config file: network firewall verbs record only the decision",
		"mgmtPorts":       "compared from the firewall config file: network firewall verbs record only the decision",
		"podCidr":         "compared from the firewall config file: network firewall verbs record only the decision",
		"inClusterPorts":  "compared from the firewall config file: network firewall verbs record only the decision",
	}
)

// Fails when a field is added to HostFirewallState without deciding whether
// drift detection compares it.
func TestFirewall_EveryRecordFieldIsComparedOrExplicitlyExcluded(t *testing.T) {
	for _, key := range yamlKeys(reflect.TypeFor[state.HostFirewallState](), "") {
		_, excluded := firewallRecordNotCompared[key]
		require.Truef(t, firewallRecordCompared[key] != excluded,
			"HostFirewallState key %q must be either compared by the firewall producer or listed in firewallRecordNotCompared", key)
	}
}

// The record field that is compared really is: a decision that disagrees with
// the host is reported on it.
func TestFirewall_TheComparedRecordFieldIsReported(t *testing.T) {
	got := Firewall(firewallStates(disabled, &state.HostFirewallObservation{TablePresent: true}))

	require.Len(t, got, 1)
	require.True(t, firewallRecordCompared[got[0].Field])
}
