// SPDX-License-Identifier: Apache-2.0

//go:build integration && linux

package policy

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/network/nftexec"
)

// These run real nft in scratch namespaces, never touching host tables. They
// pin that nft prints our render back identically, so check 1 has no false alarms.

func requireScratchNft(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root to create network namespaces")
	}
	bin, ok := nftexec.Binary()
	if !ok {
		t.Skipf("no nft binary on this host (looked for %s)", bin)
	}
	return bin
}

// itMembership is live-looking set content: prefixes, /32s, v6, and compound keys.
func itMembership() map[string][]string {
	return map[string][]string{
		"bn-publisher":   {"10.1.0.0/24", "10.2.0.1", "10.3.0.7"},
		"bn-publisher6":  {"2001:db8:1::/64"},
		"bn-partner-out": {"192.0.2.10", "198.51.100.0/25"},
		"bn-restricted":  {"203.0.113.9"},
		"bn-backfill":    {"192.0.2.20 . 40840", "192.0.2.21 . 40840"},
	}
}

func renderIT(t *testing.T) string {
	t.Helper()
	doc, err := Render(sampleBNPolicies(), itMembership(), "10.4.0.0/24", "2001:db8:c0de::/64")
	require.NoError(t, err)
	return doc
}

func listIT(t *testing.T, bin, doc string) *Listing {
	t.Helper()
	raw, err := ListInScratchNetns(context.Background(), bin, doc)
	require.NoError(t, err)
	l, err := ParseListing(raw)
	require.NoError(t, err)
	return l
}

// withoutElements drops the named set's elements from doc, so the test can add
// them back one at a time in another order, like the daemon does over time.
func withoutElements(doc, set string) string {
	re := regexp.MustCompile(`(\tset ` + regexp.QuoteMeta(set) + ` \{[^\n]*?); elements = \{ [^}]* \}`)
	return re.ReplaceAllString(doc, "$1")
}

func TestIT_ScratchListingOfOurRenderMatchesItself(t *testing.T) {
	bin := requireScratchNft(t)
	doc := renderIT(t)

	expected := listIT(t, bin, doc)
	live := listIT(t, bin, doc)

	assert.True(t, Compare(expected, live).Empty(), "%+v", Compare(expected, live))
	assert.Equal(t, FormatVersion, live.FormatVersion)
	assert.True(t, live.ForwardCounted)
	assert.NotZero(t, live.TableHandle, "nft -j must print a handle int64Of can read")
	for _, class := range []string{"publisher", "partner", "public", "reserve-egress", "reserve-ingress", "backfill-response"} {
		_, ok := live.ClassBytes[class]
		assert.True(t, ok, "no counted rule labelled %s", class)
	}
}

func TestIT_ElementsAddedLaterInAnotherOrderStillMatch(t *testing.T) {
	bin := requireScratchNft(t)
	doc := renderIT(t)

	stripped := doc
	for set := range itMembership() {
		stripped = withoutElements(stripped, set)
	}
	require.NotContains(t, stripped, "10.2.0.1")
	var b strings.Builder
	b.WriteString(stripped)
	for set, elems := range itMembership() {
		for i := len(elems) - 1; i >= 0; i-- {
			b.WriteString("add element " + TableName + " " + set + " { " + elems[i] + " }\n")
		}
	}

	d := Compare(listIT(t, bin, doc), listIT(t, bin, b.String()))
	assert.True(t, d.Empty(), "%+v", d)
}

func TestIT_HandEditedRuleIsDrift(t *testing.T) {
	bin := requireScratchNft(t)
	doc := renderIT(t)
	edited := strings.Replace(doc, "ip daddr 10.4.0.0/24 ip saddr @bn-publisher", "ip daddr 10.9.0.0/24 ip saddr @bn-publisher", 1)
	require.NotEqual(t, doc, edited)

	d := Compare(listIT(t, bin, doc), listIT(t, bin, edited))

	require.Len(t, d.Missing, 1)
	require.Len(t, d.Unexpected, 1)
	assert.Contains(t, d.Unexpected[0], "10.9.0.0")
}

func TestIT_OlderFormatReadsAsFormatOne(t *testing.T) {
	bin := requireScratchNft(t)
	doc := renderIT(t)
	old := regexp.MustCompile(` comment "weaver:[^"]*"`).ReplaceAllString(strings.ReplaceAll(doc, "counter ", ""), "")

	live := listIT(t, bin, old)

	assert.Equal(t, 1, live.FormatVersion)
	assert.False(t, live.ForwardCounted)
	expected := listIT(t, bin, doc)
	assert.False(t, Compare(expected, live).Empty())
	assert.True(t, CompareIgnoringFormat(expected, live).Empty(), "%+v", CompareIgnoringFormat(expected, live))

	edited := listIT(t, bin, strings.Replace(old, "ip daddr 10.4.0.0/24 ip saddr @bn-publisher", "ip daddr 10.9.0.0/24 ip saddr @bn-publisher", 1))
	d := CompareIgnoringFormat(expected, edited)
	require.Len(t, d.Unexpected, 1, "a hand edit on an old table is still drift")
	assert.Contains(t, d.Unexpected[0], "10.9.0.0")
}

func TestIT_ScratchNamespaceLeavesTheHostUntouched(t *testing.T) {
	bin := requireScratchNft(t)
	before, err := nftexec.TableExists(context.Background(), bin, TableName)
	require.NoError(t, err)

	listIT(t, bin, renderIT(t))

	after, err := nftexec.TableExists(context.Background(), bin, TableName)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the scratch load must never reach the host namespace")
}
