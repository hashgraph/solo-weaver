// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLaneTrees_KeepsOnlyWeaverRoots(t *testing.T) {
	out := []byte(`[
		{"kind":"htb","handle":"1:","dev":"lxc1234","root":true,"options":{"r2q":10,"default":"0x30"}},
		{"kind":"fq_codel","handle":"130:","dev":"lxc1234","parent":"1:30"},
		{"kind":"htb","handle":"1:","dev":"eth0","root":true,"options":{"default":96}},
		{"kind":"htb","handle":"8001:","dev":"eth1","root":true},
		{"kind":"htb","handle":"1:","dev":"eth2","parent":"8001:1"},
		{"kind":"clsact","handle":"ffff:","dev":"eth0","parent":"ffff:fff1"},
		{"kind":"noqueue","handle":"0:","dev":"lo","root":true}
	]`)

	trees, err := parseLaneTrees(out)
	require.NoError(t, err)
	assert.Equal(t, []LaneTree{
		{Dev: "eth0", DefaultClass: "reserve-egress"},
		{Dev: "lxc1234", DefaultClass: "reserve-ingress"},
	}, trees)
}

func TestParseLaneTrees_EmptyOutputIsAnError(t *testing.T) {
	_, err := parseLaneTrees([]byte("  \n"))
	require.Error(t, err, "empty output must not read as 'no lane trees'")
}

func TestParseLaneTrees_UnknownDefaultLeavesItBlank(t *testing.T) {
	trees, err := parseLaneTrees([]byte(`[{"kind":"htb","handle":"1:","dev":"eth0","options":{"default":"0x99"}}]`))
	require.NoError(t, err)
	require.Len(t, trees, 1)
	assert.Empty(t, trees[0].DefaultClass)
}

func TestParseFilters_DescribesEachFilterOnce(t *testing.T) {
	// tc prints a header entry and one per handle for the same filter.
	out := []byte(`[
		{"parent":"1:","protocol":"ip","pref":49152,"kind":"u32","chain":0},
		{"parent":"1:","protocol":"ip","pref":49152,"kind":"u32","chain":0,"options":{"fh":"800:"}},
		{"parent":"1:","protocol":"all","pref":1,"kind":"fw","chain":0}
	]`)

	descs, err := parseFilters(out, "1:")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"parent 1: pref 49152 protocol ip kind u32",
		"parent 1: pref 1 protocol all kind fw",
	}, descs)
}

func TestParseFilters_NoneIsEmpty(t *testing.T) {
	for _, out := range []string{"", "[]"} {
		descs, err := parseFilters([]byte(out), "1:1")
		require.NoError(t, err)
		assert.Empty(t, descs)
	}
	_, err := parseFilters([]byte("garbage"), "1:")
	require.Error(t, err)
}

func TestClassNameForID(t *testing.T) {
	name, ok := ClassNameForID("1:40")
	assert.True(t, ok)
	assert.Equal(t, "partner", name)
	for _, id := range []string{TrunkClassID, "2:40", "1:99", ""} {
		_, ok := ClassNameForID(id)
		assert.False(t, ok, id)
	}
}
