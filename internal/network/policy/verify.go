// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/joomcode/errorx"
)

// Listing is one parsed `nft -j list table inet weaver-workload-policy`. Both live
// and expected tables come from nft itself, so we never have to mimic its formatting.
type Listing struct {
	// TableHandle changes on every full reload, which also resets every counter.
	TableHandle int64
	// FormatVersion is the `forward` rule's format tag; 1 if the table predates tags.
	FormatVersion int
	// ForwardCounted is true when the `forward` rule carries a counter.
	ForwardCounted bool
	// ForwardBytes is the `forward` rule's byte counter: all traffic the table saw.
	ForwardBytes uint64
	// ClassBytes sums the byte counters of the stamp rules, keyed by class.
	ClassBytes map[string]uint64

	// entries are the table's objects, normalised for comparison.
	entries []string
	// bareEntries drop counters and weaver: comments, so another render format
	// still compares on everything else.
	bareEntries []string
}

// RulesDiff is what differs between the expected and the live table.
type RulesDiff struct {
	// Missing are expected objects the live table lacks.
	Missing []string
	// Unexpected are live objects the expected table lacks.
	Unexpected []string
	// Reordered is true when both hold the same objects in a different order.
	Reordered bool
}

// Empty reports whether the two tables match.
func (d RulesDiff) Empty() bool {
	return len(d.Missing) == 0 && len(d.Unexpected) == 0 && !d.Reordered
}

// ListTableJSON returns `nft -j list table inet weaver-workload-policy`.
func ListTableJSON(ctx context.Context, bin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, append([]string{"-j", "list", "table"}, tableArgs...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, errorx.ExternalError.Wrap(err, "nft -j list table %s failed: %s", TableName, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// ParseListing parses `nft -j list table` output into a Listing.
func ParseListing(out []byte) (*Listing, error) {
	dec := json.NewDecoder(bytes.NewReader(out))
	// Numbers stay exact: byte counters can exceed a float64's integer range.
	dec.UseNumber()
	var doc struct {
		Nftables []map[string]any `json:"nftables"`
	}
	if err := dec.Decode(&doc); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse nft JSON listing of %s", TableName)
	}

	l := &Listing{FormatVersion: 1, ClassBytes: map[string]uint64{}}
	sawTable := false
	for _, item := range doc.Nftables {
		for kind, raw := range item {
			obj, ok := raw.(map[string]any)
			if !ok || kind == "metainfo" {
				continue
			}
			switch kind {
			case "table":
				sawTable = true
				l.TableHandle = int64Of(obj["handle"])
			case "rule":
				l.readRule(obj)
			}
			entry, err := normaliseEntry(kind, obj)
			if err != nil {
				return nil, err
			}
			bare, err := normaliseEntry(kind, stripFormat(obj))
			if err != nil {
				return nil, err
			}
			l.entries = append(l.entries, entry)
			l.bareEntries = append(l.bareEntries, bare)
		}
	}
	if !sawTable {
		return nil, errorx.IllegalFormat.New("nft JSON listing of %s holds no table", TableName)
	}
	return l, nil
}

// readRule picks the counters and tags this package renders out of one rule.
func (l *Listing) readRule(rule map[string]any) {
	comment, _ := rule["comment"].(string)
	bytesSeen, counted := counterBytes(rule["expr"])

	if rule["chain"] == chainBase {
		if v, ok := ParseFormatComment(comment); ok {
			l.FormatVersion = v
		}
		if counted {
			l.ForwardCounted = true
			l.ForwardBytes += bytesSeen
		}
		return
	}
	if class, ok := ParseClassComment(comment); ok && counted {
		l.ClassBytes[class] += bytesSeen
	}
}

// Compare returns what differs between the expected and the live listing.
// Handles and counter values are ignored; set elements compare as a set.
func Compare(expected, live *Listing) RulesDiff {
	return diff(expected.entries, live.entries)
}

// CompareIgnoringFormat is Compare with counters and weaver: comments dropped
// from both sides, for a live table written by another render format.
func CompareIgnoringFormat(expected, live *Listing) RulesDiff {
	return diff(expected.bareEntries, live.bareEntries)
}

func diff(expected, live []string) RulesDiff {
	var d RulesDiff
	d.Missing = subtract(expected, live)
	d.Unexpected = subtract(live, expected)
	if len(d.Missing) == 0 && len(d.Unexpected) == 0 {
		d.Reordered = !slices.Equal(expected, live)
	}
	return d
}

// stripFormat returns a rule without its counter or weaver: comment; any other
// object is returned as is.
func stripFormat(obj map[string]any) map[string]any {
	out := maps.Clone(obj)
	if c, ok := out["comment"].(string); ok && strings.HasPrefix(c, "weaver:") {
		delete(out, "comment")
	}
	if list, ok := out["expr"].([]any); ok {
		kept := make([]any, 0, len(list))
		for _, e := range list {
			if m, ok := e.(map[string]any); ok {
				if _, isCounter := m["counter"]; isCounter {
					continue
				}
			}
			kept = append(kept, e)
		}
		out["expr"] = kept
	}
	return out
}

// subtract returns the entries of a not matched one-for-one in b, in a's order.
func subtract(a, b []string) []string {
	left := make(map[string]int, len(b))
	for _, e := range b {
		left[e]++
	}
	var out []string
	for _, e := range a {
		if left[e] > 0 {
			left[e]--
			continue
		}
		out = append(out, e)
	}
	return out
}

// normaliseEntry renders one listed object as a stable line: handles dropped,
// counters zeroed, and set elements sorted.
func normaliseEntry(kind string, obj map[string]any) (string, error) {
	obj = normaliseValue(obj).(map[string]any)
	if kind == "set" {
		if elems, ok := obj["elem"].([]any); ok {
			sortByJSON(elems)
		}
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return "", errorx.InternalError.Wrap(err, "failed to encode an nft %s object", kind)
	}
	return kind + " " + string(b), nil
}

// normaliseValue returns v with every handle dropped and every counter zeroed.
func normaliseValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			switch k {
			case "handle":
			case "counter":
				out[k] = map[string]any{"packets": 0, "bytes": 0}
			default:
				out[k] = normaliseValue(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normaliseValue(val)
		}
		return out
	default:
		return v
	}
}

// sortByJSON sorts values by their JSON encoding, in place.
func sortByJSON(vals []any) {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return string(b)
	}
	slices.SortStableFunc(vals, func(a, b any) int { return strings.Compare(enc(a), enc(b)) })
}

// counterBytes returns the byte count of the first counter in a rule's exprs.
func counterBytes(expr any) (uint64, bool) {
	list, _ := expr.([]any)
	for _, e := range list {
		m, _ := e.(map[string]any)
		c, ok := m["counter"].(map[string]any)
		if !ok {
			continue
		}
		n, _ := c["bytes"].(json.Number)
		v, _ := strconv.ParseUint(n.String(), 10, 64)
		return v, true
	}
	return 0, false
}

// int64Of reads a json.Number as an int64: 0 when it is not an integer, clamped when out of range.
func int64Of(v any) int64 {
	n, _ := v.(json.Number)
	i, _ := strconv.ParseInt(n.String(), 10, 64)
	return i
}
