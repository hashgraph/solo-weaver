// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTcQdiscJSON_IsWeaverRoot(t *testing.T) {
	for name, tc := range map[string]struct {
		in   tcQdiscJSON
		want bool
	}{
		"weaver root":                      {tcQdiscJSON{Kind: "htb", Handle: "1:", Root: boolPtr(true)}, true},
		"weaver root, bare handle":         {tcQdiscJSON{Kind: "htb", Handle: "1", Root: boolPtr(true)}, true},
		"weaver root, no root field":       {tcQdiscJSON{Kind: "htb", Handle: "1:"}, true},
		"child htb with handle 1:":         {tcQdiscJSON{Kind: "htb", Handle: "1:", Parent: "8001:1"}, false},
		"foreign htb root, other handle":   {tcQdiscJSON{Kind: "htb", Handle: "8001:"}, false},
		"handle 1 but not htb":             {tcQdiscJSON{Kind: "fq_codel", Handle: "1:"}, false},
		"weaver leaf qdisc under 1:":       {tcQdiscJSON{Kind: "fq_codel", Handle: "10:"}, false},
		"handle 10: is not handle 1:":      {tcQdiscJSON{Kind: "htb", Handle: "10:"}, false},
		"kernel default, nothing weaver's": {tcQdiscJSON{Kind: "mq", Handle: "0:"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.in.isWeaverRoot())
		})
	}
}

// TestTcQdiscJSON_ForeignRootIsNotWeaversHierarchy: a foreign root HTB must not
// read as weaver's, or the missing 1: tree never gets restored.
func TestTcQdiscJSON_ForeignRootIsNotWeaversHierarchy(t *testing.T) {
	out := []byte(`[{"kind":"htb","handle":"8001:","root":true,"refcnt":2,"options":{"r2q":10,"default":"0x0"}},` +
		`{"kind":"htb","handle":"1:","parent":"8001:1","options":{"r2q":10,"default":"0x0"}},` +
		`{"kind":"fq_codel","handle":"8002:","parent":"8001:2","options":{}}]`)
	var raw []tcQdiscJSON
	require.NoError(t, json.Unmarshal(out, &raw))

	for _, q := range raw {
		assert.False(t, q.isWeaverRoot(), "%+v", q)
	}
}

func boolPtr(b bool) *bool { return &b }

// --- qdiscRootPresent -------------------------------------------------------
//
// The whole point of the probe is a three-way answer: present, absent, or
// "cannot determine". Reading either failure below as an absence would have
// reassert rebuild the hierarchy once a minute on a host it cannot inspect.

func TestQdiscRootPresent_FindsWeaversRoot(t *testing.T) {
	out := []byte(`[{"kind":"htb","handle":"1:","root":true,"options":{"r2q":10}},` +
		`{"kind":"fq_codel","handle":"10:","parent":"1:10","options":{}}]`)

	present, err := qdiscRootPresent(out, "eth0")

	require.NoError(t, err)
	assert.True(t, present)
}

func TestQdiscRootPresent_UnshapedDeviceIsACleanAbsence(t *testing.T) {
	// A definite "no" — the state that must stay repairable.
	out := []byte(`[{"kind":"fq_codel","handle":"0:","root":true,"options":{}}]`)

	present, err := qdiscRootPresent(out, "eth0")

	require.NoError(t, err)
	assert.False(t, present)
}

func TestQdiscRootPresent_EmptyListIsACleanAbsence(t *testing.T) {
	present, err := qdiscRootPresent([]byte("[]\n"), "eth0")

	require.NoError(t, err)
	assert.False(t, present)
}

func TestQdiscRootPresent_NoOutputAtAllIsUndeterminable(t *testing.T) {
	// tc exited zero but said nothing. An empty list decodes happily from "",
	// so without this guard a silent tc would read as "the hierarchy is gone".
	for name, out := range map[string][]byte{
		"empty":      {},
		"whitespace": []byte("  \n\t\n"),
	} {
		t.Run(name, func(t *testing.T) {
			present, err := qdiscRootPresent(out, "eth0")

			require.Error(t, err)
			assert.False(t, present)
			assert.Contains(t, err.Error(), "cannot determine")
			assert.Contains(t, err.Error(), "eth0", "the operator needs to know which device")
		})
	}
}

func TestQdiscRootPresent_UnparseableOutputIsUndeterminable(t *testing.T) {
	// A tc build without JSON support, or a truncated pipe: not an absence.
	for name, out := range map[string][]byte{
		"plain text":      []byte("qdisc htb 1: root refcnt 2\n"),
		"truncated JSON":  []byte(`[{"kind":"htb","handle":"1:"`),
		"object not list": []byte(`{"kind":"htb","handle":"1:"}`),
	} {
		t.Run(name, func(t *testing.T) {
			present, err := qdiscRootPresent(out, "bond0")

			require.Error(t, err)
			assert.False(t, present)
			assert.Contains(t, err.Error(), "bond0")
		})
	}
}
