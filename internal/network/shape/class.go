// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"sort"
	"strings"
	"time"

	"github.com/joomcode/errorx"
)

// Direction constants for tc device/class mapping.
const (
	DirIngress = "ingress" // $VETH (pod traffic, applied by the daemon pod-lifecycle watcher)
	DirEgress  = "egress"  // $EGRESS physical NIC
)

// classInfo is the static per-class descriptor. Minor is the hex tc classid
// minor (e.g., "40" for partner → classid 1:40 in tc's hex notation where
// 0x40=64). Handle is the fq_codel qdisc handle string (e.g., "140" for
// handle 140: which is 1 concatenated with Minor in hex).
type classInfo struct {
	Minor  string
	Handle string
	Dir    string
}

// classInfoMap is the static name→classid/direction map for the shape package.
// It is a read-only companion to the policy classMap: Minor values are hex
// tc classid minors (e.g. 0x40=64 → "40" → classid 1:40 for partner).
var classInfoMap = map[string]classInfo{
	"publisher":         {Minor: "10", Handle: "110", Dir: DirIngress},
	"backfill-response": {Minor: "20", Handle: "120", Dir: DirIngress},
	"reserve-ingress":   {Minor: "30", Handle: "130", Dir: DirIngress},
	"partner":           {Minor: "40", Handle: "140", Dir: DirEgress},
	"public":            {Minor: "50", Handle: "150", Dir: DirEgress},
	"reserve-egress":    {Minor: "60", Handle: "160", Dir: DirEgress},
}

// lookupClassInfo resolves a class name to its classid/direction, returning an
// error naming the known classes when the name is not recognised.
func lookupClassInfo(name string) (classInfo, error) {
	c, ok := classInfoMap[name]
	if !ok {
		return classInfo{}, errorx.IllegalArgument.New(
			"unknown class %q: must be one of %s", name, strings.Join(knownClassNames(), ", "))
	}
	return c, nil
}

// knownClassNames returns class names sorted for stable error messages.
func knownClassNames() []string {
	names := make([]string, 0, len(classInfoMap))
	for n := range classInfoMap {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// knownClassNamesForDir returns class names for the given direction, sorted.
func knownClassNamesForDir(dir string) []string {
	var names []string
	for n, c := range classInfoMap {
		if c.Dir == dir {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// ClassConfig is the persisted bandwidth configuration for one named tc class.
// One JSON file per class under ClassConfigDir.
type ClassConfig struct {
	Name      string    `json:"name"`
	Rate      string    `json:"rate"`
	Ceil      string    `json:"ceil,omitempty"` // defaults to Rate when empty
	Prio      int       `json:"prio"`
	CreatedAt time.Time `json:"created_at"`
}

// effectiveCeil returns Ceil if non-empty, otherwise Rate (ceil defaults to
// rate per tc HTB semantics: a class can burst up to its rate by default).
func (c *ClassConfig) effectiveCeil() string {
	if c.Ceil != "" {
		return c.Ceil
	}
	return c.Rate
}
