// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reality

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/stretchr/testify/require"
)

func TestNewCheckers_SetsTheFirewallReader(t *testing.T) {
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)
	sm, err := state.NewStateManager(state.WithState(state.NewState(filepath.Join(t.TempDir(), "state.yaml"))), state.WithFileManager(fm))
	require.NoError(t, err)

	checkers, err := NewCheckers(sm)
	require.NoError(t, err)

	// Literals built by hand leave it nil, so no unit test runs nft; production
	// must not.
	require.NotNil(t, checkers.Firewall)
}

func TestFirewallReader_MapsWhatTheHostShows(t *testing.T) {
	read := firewallReader(func(context.Context) firewall.Observation {
		return firewall.Observation{TablePresent: true, ConfigPresent: true, Fields: []firewall.FieldObservation{
			{Field: "configFile.mgmt.cidrs", Config: []string{"10.0.0.0/8"}, Live: []string{"10.0.0.0/8", "203.0.113.7/32"}},
		}, Rules: []firewall.RuleObservation{{Name: "admin", InConfig: true}}}
	})

	require.Equal(t, &state.HostFirewallObservation{TablePresent: true, ConfigPresent: true, Fields: []state.HostFirewallFieldObservation{
		{Field: "configFile.mgmt.cidrs", Config: []string{"10.0.0.0/8"}, Live: []string{"10.0.0.0/8", "203.0.113.7/32"}},
	}, Rules: []state.HostFirewallRuleObservation{{Name: "admin", InConfig: true}}}, read(context.Background()))
}

func TestFirewallReader_UnreadHostStaysUnread(t *testing.T) {
	read := firewallReader(func(context.Context) firewall.Observation {
		return firewall.Observation{Unobserved: true, ConfigPresent: true}
	})

	require.Equal(t, &state.HostFirewallObservation{Unobserved: true, ConfigPresent: true}, read(context.Background()))
}

func TestFirewallReader_UnreadableConfigIsCarried(t *testing.T) {
	read := firewallReader(func(context.Context) firewall.Observation {
		return firewall.Observation{TablePresent: true, ConfigPresent: true, ConfigUnreadable: true}
	})

	require.Equal(t, &state.HostFirewallObservation{TablePresent: true, ConfigPresent: true, ConfigUnreadable: true}, read(context.Background()))
}

// A field added to firewall.Observation or its parts must reach the drift
// producer: it has to be added to the state type and mapped here.
func TestFirewallReader_MapsEveryField(t *testing.T) {
	var all firewall.Observation
	fillEveryField(reflect.ValueOf(&all).Elem())
	read := firewallReader(func(context.Context) firewall.Observation { return all })

	got := reflect.ValueOf(read(context.Background())).Elem()
	requireSameShape(t, reflect.ValueOf(all), got, "Observation")
}

// fillEveryField sets every field of v to a value that is not its zero value,
// giving each slice one filled element.
func fillEveryField(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			fillEveryField(v.Field(i))
		}
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		fillEveryField(v.Index(0))
	case reflect.Bool:
		v.SetBool(true)
	case reflect.String:
		v.SetString("x")
	default:
		panic("fillEveryField: unhandled kind " + v.Kind().String())
	}
}

// requireSameShape fails unless want and got have the same field names, each
// holding the same value.
func requireSameShape(t *testing.T, want, got reflect.Value, path string) {
	t.Helper()
	switch want.Kind() {
	case reflect.Struct:
		require.Equal(t, want.NumField(), got.NumField(), "%s: field count", path)
		for i := range want.NumField() {
			name := want.Type().Field(i).Name
			f := got.FieldByName(name)
			require.True(t, f.IsValid(), "%s.%s has no counterpart in the state type", path, name)
			requireSameShape(t, want.Field(i), f, path+"."+name)
		}
	case reflect.Slice:
		require.Equal(t, want.Len(), got.Len(), "%s: length", path)
		for i := range want.Len() {
			requireSameShape(t, want.Index(i), got.Index(i), path+"[]")
		}
	default:
		require.Equal(t, want.Interface(), got.Interface(), "%s is not mapped", path)
	}
}
