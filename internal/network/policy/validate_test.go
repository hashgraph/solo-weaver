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
			name:    "deny with ports rejected",
			policy:  &Policy{Name: "x", Action: ActionDeny, Ports: []string{"40840"}},
			wantErr: "--ports does not apply to --deny",
		},
		{
			name:    "from-entity world with cidrs rejected",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "public", FromEntityWorld: true},
			cidrs:   []string{"10.0.0.0/8"},
			wantErr: "mutually exclusive with --cidrs",
		},
		{
			name:    "from-entity world on deny rejected",
			policy:  &Policy{Name: "x", Action: ActionDeny, FromEntityWorld: true},
			wantErr: "--from-entity world does not apply to --deny",
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
			name:    "ipv6 cidr rejected",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "publisher"},
			cidrs:   []string{"2001:db8::/32"},
			wantErr: "IPv6 is not yet supported",
		},
		{
			name:    "reply-stamp cidr without port rejected",
			policy:  &Policy{Name: "x", Action: ActionStamp, Stamp: "reserve-egress", ReplyStamp: "backfill-response"},
			cidrs:   []string{"10.30.5.7"},
			wantErr: "require ip:port pairs",
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
