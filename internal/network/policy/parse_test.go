// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractPodCIDRs(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want []string
	}{
		{
			name: "ingress rule",
			doc:  "ip daddr 10.4.0.0/24 ip saddr @bn-publisher tcp dport @bn-publisher_ports meta priority set 0x10010 accept",
			want: []string{"10.4.0.0/24"},
		},
		{
			name: "egress rule",
			doc:  "ip saddr 10.4.0.0/24 ip daddr @bn-partner-out tcp sport @bn-partner-out_ports meta priority set 0x10040 accept",
			want: []string{"10.4.0.0/24"},
		},
		{
			name: "compound reply-stamp forward rule",
			doc:  "ip saddr 10.4.0.0/24 ip daddr . tcp dport @bn-backfill ct mark set 0x20 meta priority set 0x10060 accept",
			want: []string{"10.4.0.0/24"},
		},
		{
			name: "dual-stack recovers both families",
			doc:  "ip saddr 10.4.0.0/24 ip daddr @bn-partner-out meta priority set 0x10040 accept\nip6 saddr 2001:db8:c0de::/64 ip6 daddr @bn-partner-out6 meta priority set 0x10040 accept",
			want: []string{"10.4.0.0/24", "2001:db8:c0de::/64"},
		},
		{
			name: "v6-only egress rule",
			doc:  "ip6 saddr 2001:db8:c0de::/64 ip6 daddr @bn-partner-out6 meta priority set 0x10040 accept",
			want: []string{"2001:db8:c0de::/64"},
		},
		{
			name: "v6 set reference must not be mistaken for a pod CIDR",
			doc:  "ip6 saddr @bn-restricted6 drop\nip6 daddr @bn-restricted6 drop",
			want: nil,
		},
		{
			name: "deny-only chain has no literal CIDR to recover",
			doc:  "ip saddr @bn-restricted drop\nip daddr @bn-restricted drop",
			want: nil,
		},
		{
			name: "empty document",
			doc:  "",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ExtractPodCIDRs(tt.doc))
		})
	}
}

func TestExtractPodCIDRs_FromGoldenFile(t *testing.T) {
	// The real render output must round-trip: whatever Render/renderStampRule
	// wrote for the sample BN install set must be recoverable byte-for-byte.
	doc, err := Render(sampleBNPolicies(), nil, "10.4.0.0/24")
	require.NoError(t, err)
	require.Equal(t, []string{"10.4.0.0/24"}, ExtractPodCIDRs(doc))

	// Dual-stack: both families round-trip out of the rendered chain.
	dual, err := Render(sampleBNPolicies(), nil, "10.4.0.0/24", "2001:db8:c0de::/64")
	require.NoError(t, err)
	require.Equal(t, []string{"10.4.0.0/24", "2001:db8:c0de::/64"}, ExtractPodCIDRs(dual))
}
