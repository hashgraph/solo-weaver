// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		policy  *Policy
		cidrs   []string
		wantErr string // "" means valid
	}{
		{
			name:   "valid stamp ingress",
			policy: &Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}},
			cidrs:  []string{"10.1.0.1/32"},
		},
		{
			name:   "valid deny",
			policy: &Policy{Name: "bn-restricted", Action: ActionDeny},
			cidrs:  []string{"10.99.0.0/16"},
		},
		{
			name:   "valid reply-stamp egress with ip:port cidrs",
			policy: &Policy{Name: "bn-backfill", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
			cidrs:  []string{"10.30.5.7:43473"},
		},
		{
			name:   "valid from-entity world fallthrough",
			policy: &Policy{Name: "bn-public-out", Action: ActionStamp, Stamp: "public", FromEntityWorld: true, Ports: []string{"40980"}},
		},
		{
			name:    "empty name",
			policy:  &Policy{Action: ActionStamp, Stamp: "publisher"},
			wantErr: "invalid --name",
		},
		{
			name:    "no action",
			policy:  &Policy{Name: "x"},
			wantErr: "exactly one of --stamp or --deny",
		},
		{
			name:    "stamp unknown class",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "bogus"},
			wantErr: "unknown class",
		},
		{
			name:    "reply-stamp on ingress-class stamp rejected",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "publisher", ReplyStamp: "backfill-response"},
			wantErr: "only valid when --stamp resolves to an egress class",
		},
		{
			name:    "reply-stamp same-direction class rejected",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "partner"},
			wantErr: "must resolve to an ingress class",
		},
		{
			name:    "deny with stamp rejected",
			policy:  &Policy{Name: "x", Action: ActionDeny, Stamp: "publisher"},
			wantErr: "mutually exclusive",
		},
		{
			name:    "deny with direction rejected",
			policy:  &Policy{Name: "x", Action: ActionDeny, Direction: DirectionIngress},
			wantErr: "--direction does not apply to --deny",
		},
		{
			name:   "valid deny narrowed by cidrs and ports",
			policy: &Policy{Name: "x", Action: ActionDeny, Ports: []string{"40840"}},
			cidrs:  []string{"10.99.0.0/16"},
		},
		{
			name:   "valid deny narrowed by ports alone",
			policy: &Policy{Name: "bn-health", Action: ActionDeny, FromEntityWorld: true, Ports: []string{"40983"}},
		},
		{
			name:    "from-entity world with cidrs rejected",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "public", FromEntityWorld: true},
			cidrs:   []string{"10.0.0.0/8"},
			wantErr: "mutually exclusive with --cidrs",
		},
		{
			// Neither an IP set nor a port set left to match on: the rule would
			// render as a bare `drop` and take the node's forwarding down.
			name:    "from-entity world on deny with no ports rejected",
			policy:  &Policy{Name: "x", Action: ActionDeny, FromEntityWorld: true},
			wantErr: "--deny with --from-entity world requires --ports",
		},
		{
			name:    "invalid port",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "publisher", Ports: []string{"70000"}},
			wantErr: "invalid --ports entry",
		},
		{
			name:    "invalid cidr",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "publisher"},
			cidrs:   []string{"not-a-cidr"},
			wantErr: "invalid --cidrs entry",
		},
		{
			name:    "ipv6 cidr accepted (dual-stack)",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "publisher"},
			cidrs:   []string{"2001:db8::/32"},
			wantErr: "",
		},
		{
			name:    "mixed v4/v6 cidrs accepted",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "publisher"},
			cidrs:   []string{"10.1.0.0/16", "2001:db8::/32"},
			wantErr: "",
		},
		{
			name:    "reply-stamp accepts bracketed ipv6 ip:port",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
			cidrs:   []string{"[2001:db8::7]:443"},
			wantErr: "",
		},
		{
			name:    "reply-stamp cidr without port rejected",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
			cidrs:   []string{"10.30.5.7"},
			wantErr: "require ip:port pairs",
		},
		{
			name:    "domain name refused with the statusz-ownership reason",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "publisher"},
			cidrs:   []string{"jump.corp.example.com"},
			wantErr: "replaces the policy sets it owns on every poll",
		},
		{
			name:    "domain name points at the surface that takes names",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "publisher"},
			cidrs:   []string{"jump.corp.example.com"},
			wantErr: "network firewall --mgmt-cidrs/--blocked-cidrs",
		},
		{
			name:    "bare domain name on a compound set gets the same reason",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
			cidrs:   []string{"jump.corp.example.com"},
			wantErr: "replaces the policy sets it owns on every poll",
		},
		{
			name:    "domain:port on a compound set gets the same reason",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
			cidrs:   []string{"jump.corp.example.com:443"},
			wantErr: "replaces the policy sets it owns on every poll",
		},
		{
			name:    "malformed address is not treated as a name",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "publisher"},
			cidrs:   []string{"not-a-cidr"},
			wantErr: "invalid CIDR: not-a-cidr",
		},
		{
			name:    "maskless ip still asks for a prefix length",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "publisher"},
			cidrs:   []string{"10.1.0.1"},
			wantErr: "invalid CIDR: 10.1.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate(tt.cidrs)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidate_DerivesDirectionFromStamp(t *testing.T) {
	p := &Policy{Name: "bn-publisher", Action: ActionStamp, Stamp: "publisher", Ports: []string{"40840"}}
	require.NoError(t, p.Validate(nil))
	require.Equal(t, DirectionIngress, p.Direction)

	p = &Policy{Name: "bn-partner-out", Action: ActionStamp, Stamp: "partner", Ports: []string{"40980"}}
	require.NoError(t, p.Validate(nil))
	require.Equal(t, DirectionEgress, p.Direction)
}

func TestLookupClass(t *testing.T) {
	c, err := lookupClass("publisher")
	require.NoError(t, err)
	require.Equal(t, uint32(0x10010), c.Priority)
	require.Equal(t, uint32(0x10), c.Mark)

	_, err = lookupClass("nope")
	require.ErrorContains(t, err, "unknown class")
}
