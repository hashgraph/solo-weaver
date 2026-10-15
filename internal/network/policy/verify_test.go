// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listingJSON builds an `nft -j list table` document in the shape nft prints.
// handleBase and counter scale vary what Compare must ignore.
func listingJSON(handleBase, scale int, elems string, publisherRule string) []byte {
	h := func(n int) int { return handleBase + n }
	objs := []string{
		`{"metainfo":{"version":"1.0.9","release_name":"Old Doc Yak #3","json_schema_version":1}}`,
		fmt.Sprintf(`{"table":{"family":"inet","name":"weaver-workload-policy","handle":%d}}`, h(0)),
		fmt.Sprintf(`{"set":{"family":"inet","name":"bn-publisher","table":"weaver-workload-policy","type":"ipv4_addr","handle":%d,"flags":["interval"],"elem":[%s]}}`, h(1), elems),
		fmt.Sprintf(`{"chain":{"family":"inet","table":"weaver-workload-policy","name":"forward","handle":%d,"type":"filter","hook":"forward","prio":0,"policy":"accept"}}`, h(2)),
		fmt.Sprintf(`{"chain":{"family":"inet","table":"weaver-workload-policy","name":"forward_ipv4","handle":%d}}`, h(3)),
		fmt.Sprintf(`{"rule":{"family":"inet","table":"weaver-workload-policy","chain":"forward","handle":%d,"comment":"weaver:format=2","expr":[{"counter":{"packets":%d,"bytes":%d}},{"vmap":{"key":{"meta":{"key":"nfproto"}},"data":{"set":[["ipv4",{"jump":{"target":"forward_ipv4"}}]]}}}]}}`, h(4), 10*scale, 5000*scale),
		fmt.Sprintf(`{"rule":{"family":"inet","table":"weaver-workload-policy","chain":"forward_ipv4","handle":%d,"comment":"weaver:class=publisher","expr":[%s,{"counter":{"packets":%d,"bytes":%d}},{"mangle":{"key":{"meta":{"key":"priority"}},"value":"1:10"}},{"accept":null}]}}`, h(5), publisherRule, 3*scale, 300*scale),
		fmt.Sprintf(`{"rule":{"family":"inet","table":"weaver-workload-policy","chain":"forward_ipv4","handle":%d,"comment":"weaver:class=partner","expr":[{"counter":{"packets":%d,"bytes":%d}},{"accept":null}]}}`, h(6), scale, 700*scale),
	}
	return []byte(`{"nftables":[` + strings.Join(objs, ",") + `]}`)
}

const (
	pubMatch      = `{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":{"prefix":{"addr":"10.4.0.0","len":24}}}}`
	pubMatchWrong = `{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":{"prefix":{"addr":"10.9.0.0","len":24}}}}`
	elemsA        = `{"prefix":{"addr":"10.1.0.0","len":24}},"10.2.0.1"`
	elemsB        = `"10.2.0.1",{"prefix":{"addr":"10.1.0.0","len":24}}`
)

func TestParseListing_ReadsCountersAndTags(t *testing.T) {
	l, err := ParseListing(listingJSON(7, 1, elemsA, pubMatch))
	require.NoError(t, err)

	assert.Equal(t, int64(7), l.TableHandle)
	assert.Equal(t, 2, l.FormatVersion)
	assert.True(t, l.ForwardCounted)
	assert.Equal(t, uint64(5000), l.ForwardBytes)
	assert.Equal(t, map[string]uint64{"publisher": 300, "partner": 700}, l.ClassBytes)
}

func TestParseListing_UntaggedTableIsFormatOne(t *testing.T) {
	raw := strings.ReplaceAll(string(listingJSON(1, 1, elemsA, pubMatch)), `"comment":"weaver:format=2",`, "")
	l, err := ParseListing([]byte(raw))
	require.NoError(t, err)
	assert.Equal(t, 1, l.FormatVersion)
}

func TestParseListing_RejectsGarbageAndTablelessOutput(t *testing.T) {
	_, err := ParseListing([]byte("not json"))
	require.Error(t, err)
	_, err = ParseListing([]byte(`{"nftables":[{"metainfo":{}}]}`))
	require.Error(t, err)
}

// A table reloaded elsewhere differs only in handles, counters and element order.
func TestCompare_IgnoresHandlesCountersAndElementOrder(t *testing.T) {
	expected, err := ParseListing(listingJSON(1, 0, elemsA, pubMatch))
	require.NoError(t, err)
	live, err := ParseListing(listingJSON(40, 9, elemsB, pubMatch))
	require.NoError(t, err)

	assert.True(t, Compare(expected, live).Empty())
}

func TestCompare_ReportsTheRuleThatChanged(t *testing.T) {
	expected, err := ParseListing(listingJSON(1, 0, elemsA, pubMatch))
	require.NoError(t, err)
	live, err := ParseListing(listingJSON(1, 0, elemsA, pubMatchWrong))
	require.NoError(t, err)

	d := Compare(expected, live)
	require.Len(t, d.Missing, 1)
	require.Len(t, d.Unexpected, 1)
	assert.Contains(t, d.Missing[0], "10.4.0.0")
	assert.Contains(t, d.Unexpected[0], "10.9.0.0")
	assert.False(t, d.Reordered)
}

func TestCompare_ReportsAChangedSetMember(t *testing.T) {
	expected, err := ParseListing(listingJSON(1, 0, elemsA, pubMatch))
	require.NoError(t, err)
	live, err := ParseListing(listingJSON(1, 0, `"10.2.0.1"`, pubMatch))
	require.NoError(t, err)

	d := Compare(expected, live)
	require.Len(t, d.Missing, 1)
	assert.True(t, strings.HasPrefix(d.Missing[0], "set "))
}

func TestCompare_ReportsReorderedRules(t *testing.T) {
	expected, err := ParseListing(listingJSON(1, 0, elemsA, pubMatch))
	require.NoError(t, err)
	live, err := ParseListing(listingJSON(1, 0, elemsA, pubMatch))
	require.NoError(t, err)
	n := len(live.entries)
	live.entries[n-1], live.entries[n-2] = live.entries[n-2], live.entries[n-1]

	d := Compare(expected, live)
	assert.Empty(t, d.Missing)
	assert.Empty(t, d.Unexpected)
	assert.True(t, d.Reordered)
	assert.False(t, d.Empty())
}

func TestFormatAndClassComments_RoundTrip(t *testing.T) {
	v, ok := ParseFormatComment(FormatComment(FormatVersion))
	assert.True(t, ok)
	assert.Equal(t, FormatVersion, v)
	_, ok = ParseFormatComment("weaver:format=x")
	assert.False(t, ok)

	c, ok := ParseClassComment(ClassComment("partner"))
	assert.True(t, ok)
	assert.Equal(t, "partner", c)
	_, ok = ParseClassComment("hand-written")
	assert.False(t, ok)
}

// Check 3 sums counters per class, so every stamp rule must carry a label.
func TestRender_EveryStampRuleIsCountedAndLabelled(t *testing.T) {
	doc, err := Render(sampleBNPolicies(), nil, "10.4.0.0/24", "2001:db8::/64")
	require.NoError(t, err)

	stamps := 0
	for line := range strings.SplitSeq(doc, "\n") {
		if !strings.Contains(line, "meta priority set") {
			continue
		}
		stamps++
		assert.Contains(t, line, " counter ", line)
		assert.Contains(t, line, `comment "weaver:class=`, line)
	}
	assert.Positive(t, stamps)
	assert.Contains(t, doc, `counter meta nfproto vmap`)
	assert.Contains(t, doc, `comment "`+FormatComment(FormatVersion)+`"`)
}

func TestCompareIgnoringFormat_DropsOnlyCountersAndWeaverComments(t *testing.T) {
	expected, err := ParseListing(listingJSON(1, 0, elemsA, pubMatch))
	require.NoError(t, err)
	oldRaw := strings.NewReplacer(
		`{"counter":{"packets":0,"bytes":0}},`, "",
		`,{"counter":{"packets":0,"bytes":0}}`, "",
		`,"comment":"weaver:class=publisher"`, "",
		`,"comment":"weaver:class=partner"`, "",
		`,"comment":"weaver:format=2"`, "",
	).Replace(string(listingJSON(1, 0, elemsA, pubMatch)))
	old, err := ParseListing([]byte(oldRaw))
	require.NoError(t, err)
	require.Equal(t, 1, old.FormatVersion)

	assert.False(t, Compare(expected, old).Empty())
	assert.True(t, CompareIgnoringFormat(expected, old).Empty(), "%+v", CompareIgnoringFormat(expected, old))

	edited, err := ParseListing([]byte(strings.Replace(oldRaw, "10.4.0.0", "10.9.0.0", 1)))
	require.NoError(t, err)
	assert.False(t, CompareIgnoringFormat(expected, edited).Empty())
}

func TestReloadTable_LoadsTheDocumentAndPersistsIt(t *testing.T) {
	r := newFakeRunner()
	m, nftPath, _ := newTestManager(t, r)

	require.NoError(t, m.ReloadTable(context.Background(), "doc v2\n"))

	assert.Equal(t, "doc v2\n", r.applied)
	got, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	assert.Equal(t, "doc v2\n", string(got), "the boot artifact must match the live table")
}

func TestReloadTable_FailedLoadLeavesTheArtifactAlone(t *testing.T) {
	r := newFakeRunner()
	r.applyErr = errors.New("nft -f failed")
	m, nftPath, _ := newTestManager(t, r)
	require.NoError(t, os.WriteFile(nftPath, []byte("old\n"), 0o644))

	require.Error(t, m.ReloadTable(context.Background(), "new\n"))

	got, err := os.ReadFile(nftPath)
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(got))
}

func TestInt64Of(t *testing.T) {
	for name, tc := range map[string]struct {
		in   any
		want int64
	}{
		"number":       {json.Number("7"), 7},
		"negative":     {json.Number("-3"), -3},
		"max int64":    {json.Number("9223372036854775807"), math.MaxInt64},
		"above int64":  {json.Number("99999999999999999999"), math.MaxInt64},
		"below int64":  {json.Number("-99999999999999999999"), math.MinInt64},
		"fraction":     {json.Number("7.5"), 0},
		"exponent":     {json.Number("1e3"), 0},
		"empty number": {json.Number(""), 0},
		"plain string": {"7", 0},
		"float64":      {float64(7), 0},
		"int":          {7, 0},
		"nil":          {nil, 0},
		"object":       {map[string]any{"n": json.Number("7")}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, int64Of(tc.in))
		})
	}
}

// A handle nft prints in an unexpected shape reads as 0, never an error: the
// rest of the listing still has to be compared.
func TestParseListing_OddTableHandles(t *testing.T) {
	const table = `{"table":{"family":"inet","name":"weaver-workload-policy","handle":7}}`
	for name, tc := range map[string]struct {
		table string
		want  int64
	}{
		"missing":  {`{"table":{"family":"inet","name":"weaver-workload-policy"}}`, 0},
		"string":   {`{"table":{"family":"inet","name":"weaver-workload-policy","handle":"7"}}`, 0},
		"fraction": {`{"table":{"family":"inet","name":"weaver-workload-policy","handle":7.5}}`, 0},
		"null":     {`{"table":{"family":"inet","name":"weaver-workload-policy","handle":null}}`, 0},
		"big":      {`{"table":{"family":"inet","name":"weaver-workload-policy","handle":4294967296}}`, 1 << 32},
	} {
		t.Run(name, func(t *testing.T) {
			raw := strings.Replace(string(listingJSON(7, 1, elemsA, pubMatch)), table, tc.table, 1)
			require.NotEqual(t, string(listingJSON(7, 1, elemsA, pubMatch)), raw)

			l, err := ParseListing([]byte(raw))

			require.NoError(t, err)
			assert.Equal(t, tc.want, l.TableHandle)
			assert.Equal(t, uint64(5000), l.ForwardBytes, "the rest of the listing is still read")
		})
	}
}
