// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/joomcode/errorx"
)

// TrunkClassID is the class every per-class lane hangs off; its counters are
// the total traffic through a lane tree.
const TrunkClassID = "1:1"

// FilterParents are the only tc filter attach points HTB consults in weaver's
// tree. Cilium's clsact filters live elsewhere and are expected.
var FilterParents = []string{"1:", "1:1"}

// LaneTree is one device carrying weaver's `root handle 1: htb` qdisc.
type LaneTree struct {
	Dev string
	// DefaultClass is the class unstamped traffic lands in, "" if unknown.
	DefaultClass string
}

// ClassNameForID returns the class name for a tc classid such as "1:40".
func ClassNameForID(classID string) (string, bool) {
	minor, ok := strings.CutPrefix(classID, "1:")
	if !ok {
		return "", false
	}
	for name, c := range classInfoMap {
		if c.Minor == minor {
			return name, true
		}
	}
	return "", false
}

// laneQdiscJSON is the subset of `tc -j qdisc show` (all devices) read here.
type laneQdiscJSON struct {
	tcQdiscJSON
	Dev     string `json:"dev"`
	Options struct {
		Default json.RawMessage `json:"default"`
	} `json:"options"`
}

// tcQdiscJSON is the subset of `tc -j qdisc show` output the probe reads.
// iproute2 emits "root": true or "parent", never "root": false.
type tcQdiscJSON struct {
	Kind   string `json:"kind"`
	Handle string `json:"handle"`
	Parent string `json:"parent"`
	Root   *bool  `json:"root"`
}

// isWeaverRoot reports whether this entry is weaver's `root handle 1: htb`
// qdisc, not a foreign hierarchy's child HTB that happens to reuse handle 1:.
func (q tcQdiscJSON) isWeaverRoot() bool {
	if q.Kind != "htb" || strings.TrimSuffix(q.Handle, ":") != "1" {
		return false
	}
	if q.Parent != "" {
		return false
	}
	// A missing "root" is tolerated for iproute2 builds that omit the field.
	return q.Root == nil || *q.Root
}

// parseLaneTrees returns the devices in `tc -j qdisc show` output that carry
// weaver's lane tree, sorted by name.
func parseLaneTrees(out []byte) ([]LaneTree, error) {
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, errorx.ExternalError.New("tc -j qdisc show produced no output")
	}
	var raw []laneQdiscJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to parse tc qdisc JSON")
	}
	var trees []LaneTree
	for _, q := range raw {
		if q.Dev == "" || !q.isWeaverRoot() {
			continue
		}
		t := LaneTree{Dev: q.Dev}
		if minor, ok := htbDefaultMinor(q.Options.Default); ok {
			t.DefaultClass, _ = ClassNameForID("1:" + minor)
		}
		trees = append(trees, t)
	}
	slices.SortFunc(trees, func(a, b LaneTree) int { return strings.Compare(a.Dev, b.Dev) })
	return trees, nil
}

// htbDefaultMinor decodes HTB's `default` option, which iproute2 prints as a
// hex string ("0x30") or, on older builds, as a number.
func htbDefaultMinor(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		v, err := strconv.ParseUint(s, 0, 32)
		if err != nil {
			return "", false
		}
		return strconv.FormatUint(v, 16), true
	}
	var n uint64
	if json.Unmarshal(raw, &n) == nil {
		return strconv.FormatUint(n, 16), true
	}
	return "", false
}

// tcFilterJSON is the subset of `tc -j filter show` output read here.
type tcFilterJSON struct {
	Kind     string `json:"kind"`
	Protocol string `json:"protocol"`
	Pref     uint32 `json:"pref"`
}

// parseFilters describes every filter in `tc -j filter show ... parent <p>`
// output, one line per (pref, protocol, kind). Empty output means none.
func parseFilters(out []byte, parent string) ([]string, error) {
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var raw []tcFilterJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to parse tc filter JSON for parent %s", parent)
	}
	var descs []string
	for _, f := range raw {
		if f.Kind == "" {
			continue
		}
		d := fmt.Sprintf("parent %s pref %d protocol %s kind %s", parent, f.Pref, f.Protocol, f.Kind)
		if !slices.Contains(descs, d) {
			descs = append(descs, d)
		}
	}
	return descs, nil
}
