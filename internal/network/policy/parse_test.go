// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractPodCIDR(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{
			name: "ingress rule",
			doc:  "ip daddr 10.4.0.0/24 ip saddr @bn-publisher tcp dport @bn-publisher_ports meta priority set 0x10010 accept",
			want: "10.4.0.0/24",
		},
		{
			name: "egress rule",
			doc:  "ip saddr 10.4.0.0/24 ip daddr @bn-partner-out tcp sport @bn-partner-out_ports meta priority set 0x10040 accept",
			want: "10.4.0.0/24",
		},
		{
			name: "compound reply-stamp forward rule",
			doc:  "ip saddr 10.4.0.0/24 ip daddr . tcp dport @bn-backfill ct mark set 0x20 meta priority set 0x10060 accept",
			want: "10.4.0.0/24",
		},
		{
			name: "deny-only chain has no literal CIDR to recover",
			doc:  "ip saddr @bn-restricted drop\nip daddr @bn-restricted drop",
			want: "",
		},
		{
			name: "empty document",
			doc:  "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ExtractPodCIDR(tt.doc))
		})
	}
}

func TestExtractPodCIDR_FromGoldenFile(t *testing.T) {
	// The real render output must round-trip: whatever Render/renderStampRule
	// wrote for the sample BN install set must be recoverable byte-for-byte.
	doc, err := Render(sampleBNPolicies(), "10.4.0.0/24")
	require.NoError(t, err)
	require.Equal(t, "10.4.0.0/24", ExtractPodCIDR(doc))
}
