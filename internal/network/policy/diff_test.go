// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCompoundElement(t *testing.T) {
	tok, err := CompoundElement("10.30.5.7:43473")
	require.NoError(t, err)
	require.Equal(t, "10.30.5.7 . 43473", tok)

	// Port leading zeros are normalized away.
	tok, err = CompoundElement("10.30.5.7:00080")
	require.NoError(t, err)
	require.Equal(t, "10.30.5.7 . 80", tok)

	for _, bad := range []string{
		"not-an-ip-port",   // no port
		"10.30.5.7",        // missing port
		"10.30.5.7:-1",     // negative port
		"10.30.5.7:0",      // port below range
		"10.30.5.7:99999",  // port above range
		"10.30.5.7:http",   // non-numeric port
		"::1:80",           // IPv6 host
		"[2001:db8::1]:80", // IPv6 host
	} {
		_, err := CompoundElement(bad)
		require.Error(t, err, "expected %q to be rejected", bad)
	}
}

func TestCanonicalizeElements(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "numeric order, not lexical",
			in:   []string{"10.1.0.10/32", "10.1.0.2/32", "10.1.0.1/32"},
			want: []string{"10.1.0.1", "10.1.0.2", "10.1.0.10"},
		},
		{
			name: "single-host /32 collapses to bare, wider mask kept",
			in:   []string{"10.4.0.0/24", "10.1.0.5/32", "10.1.0.5"},
			want: []string{"10.1.0.5", "10.4.0.0/24"},
		},
		{
			name: "dedup after normalization",
			in:   []string{"10.1.0.2/32", "10.1.0.2", "10.1.0.2/32"},
			want: []string{"10.1.0.2"},
		},
		{
			name: "compound sorted by ip then port",
			in:   []string{"10.30.5.7 . 500", "10.30.5.7 . 100", "10.30.5.2 . 900"},
			want: []string{"10.30.5.2 . 900", "10.30.5.7 . 100", "10.30.5.7 . 500"},
		},
		{
			name: "prefix mask is canonicalized to network address",
			in:   []string{"10.4.0.5/24"},
			want: []string{"10.4.0.0/24"},
		},
		{
			name: "unparseable tokens preserved and sorted last",
			in:   []string{"garbage", "10.1.0.2/32"},
			want: []string{"10.1.0.2", "garbage"},
		},
		{
			name: "out-of-range compound port is not authoritative, sorts last",
			in:   []string{"10.30.5.7 . 99999", "10.1.0.2/32"},
			want: []string{"10.1.0.2", "10.30.5.7 . 99999"},
		},
		{
			name: "IPv6 element is not IPv4, treated as unparseable",
			in:   []string{"2001:db8::1", "10.1.0.2/32"},
			want: []string{"10.1.0.2", "2001:db8::1"},
		},
		{
			name: "empty input",
			in:   nil,
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalizeElements(tt.in)
			require.Equal(t, tt.want, got)
			// Idempotent: canonicalizing the output changes nothing.
			require.Equal(t, got, CanonicalizeElements(got))
		})
	}
}

func TestDiffElements(t *testing.T) {
	tests := []struct {
		name        string
		desired     []string
		live        []string
		wantAdds    []string
		wantDeletes []string
	}{
		{
			name:     "adds only",
			desired:  []string{"10.1.0.1/32", "10.1.0.2/32"},
			live:     nil,
			wantAdds: []string{"10.1.0.1", "10.1.0.2"},
		},
		{
			name:        "deletes only (empty desired clears set)",
			desired:     nil,
			live:        []string{"10.1.0.1/32"},
			wantDeletes: []string{"10.1.0.1"},
		},
		{
			name:        "mixed add and delete",
			desired:     []string{"10.1.0.2/32", "10.1.0.3/32"},
			live:        []string{"10.1.0.1/32", "10.1.0.2/32"},
			wantAdds:    []string{"10.1.0.3"},
			wantDeletes: []string{"10.1.0.1"},
		},
		{
			name:    "no-op despite spelling difference (/32 vs bare, unsorted)",
			desired: []string{"10.1.0.10/32", "10.1.0.2"},
			live:    []string{"10.1.0.2/32", "10.1.0.10"},
		},
		{
			name:     "compound elements diff",
			desired:  []string{"10.30.5.8 . 43473"},
			live:     []string{"10.30.5.7 . 43473"},
			wantAdds: []string{"10.30.5.8 . 43473"},
			wantDeletes: []string{
				"10.30.5.7 . 43473",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := DiffElements(tt.desired, tt.live)
			require.Equal(t, tt.wantAdds, d.Adds)
			require.Equal(t, tt.wantDeletes, d.Deletes)
			require.Equal(t, len(tt.wantAdds) == 0 && len(tt.wantDeletes) == 0, d.Empty())
		})
	}
}
