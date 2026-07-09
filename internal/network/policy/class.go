// SPDX-License-Identifier: Apache-2.0

package policy

import (
	"sort"
	"strings"

	"github.com/joomcode/errorx"
)

// class is the static definition of a QoS priority class: its conntrack mark,
// the skb->priority value nft stamps for it, and the traffic direction it
// classifies. The values are the stable mark map from design §5 (fixed in
// code, never reconciled), keyed by the class names declared by
// `network shape create` (design §8.4.3).
//
// The priority is always mark | 0x10000 — the classid `1:<mark>` encoded as an
// skb->priority (major 1, minor <mark>) so HTB classifies natively without a tc
// filter (§5.1, §7.2.4 property 5). Both fields are stored explicitly rather
// than derived so this table reads as a direct transcription of the §5 map.
//
// Direction is fixed per class, not an independent parameter: §7.2.4's worked
// ruleset never uses a class in both directions (property 6). `publisher` and
// `reserve-ingress` are DirectionIngress; `partner`, `public`, and
// `reserve-egress` are DirectionEgress. `backfill-response` is the one
// exception — it is never a forward `--stamp`, only a `--reply-stamp` target,
// and its priority is applied on the ingress leg (the reply), so it is also
// DirectionIngress despite being declared alongside an egress-direction
// forward policy.
type class struct {
	// Mark is the conntrack mark (§5 "Mark" column), used only on --reply-stamp
	// forward rules so the ingress restore rule has something to read back.
	Mark uint32
	// Priority is the skb->priority stamped via `meta priority set` (§5
	// "Priority" column).
	Priority uint32
	// Direction is the traffic direction this class classifies (§5 "Direction"
	// column). `network policy create` derives `Policy.Direction` from this
	// field instead of taking it as a separate flag.
	Direction Direction
}

// classMap is the stable class→mark/priority/direction map (design §5). Story
// 1.4 (`network shape`) sets each class's bandwidth; the name→priority
// encoding itself is fixed here in code and does not depend on shape state.
// `--stamp` / `--reply-stamp` referencing a name absent from this map is a
// create-time error.
var classMap = map[string]class{
	"publisher":         {Mark: 0x10, Priority: 0x10010, Direction: DirectionIngress},
	"backfill-response": {Mark: 0x20, Priority: 0x10020, Direction: DirectionIngress},
	"reserve-ingress":   {Mark: 0x30, Priority: 0x10030, Direction: DirectionIngress},
	"partner":           {Mark: 0x40, Priority: 0x10040, Direction: DirectionEgress},
	"public":            {Mark: 0x50, Priority: 0x10050, Direction: DirectionEgress},
	"reserve-egress":    {Mark: 0x60, Priority: 0x10060, Direction: DirectionEgress},
}

// lookupClass resolves a class name to its mark/priority, returning an
// IllegalArgument error naming the known classes when the reference is
// undeclared (design §8.4.2: "referencing an undeclared class is an error").
func lookupClass(name string) (class, error) {
	c, ok := classMap[name]
	if !ok {
		return class{}, errorx.IllegalArgument.New(
			"unknown class %q: must be one of %s", name, strings.Join(knownClasses(), ", "))
	}
	return c, nil
}

// knownClasses returns the declared class names in sorted order for stable
// error messages.
func knownClasses() []string {
	names := make([]string, 0, len(classMap))
	for n := range classMap {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
