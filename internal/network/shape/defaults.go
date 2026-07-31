// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"fmt"
	"time"

	"github.com/joomcode/errorx"
)

// defaultClassSpec describes one HTB class in a default shaping profile as
// proportions of the device trunk rate. rate is always ratePct% of the trunk;
// ceil is either ceilPct% of the trunk, or — when ceilFull is set — the trunk
// rate verbatim (preserving the operator's original unit, e.g. "1gbit" rather
// than "1000mbit").
type defaultClassSpec struct {
	name     string
	ratePct  int
	ceilPct  int
	ceilFull bool
	prio     int
}

// defaultProfile is the default device root + class set for one direction.
type defaultProfile struct {
	dir          string
	defaultClass string
	classes      []defaultClassSpec
}

// egressProfile: three egress classes at partner 40%/70%, public 30%/70%,
// reserve-egress 30%/100%.
var egressProfile = defaultProfile{
	dir:          DirEgress,
	defaultClass: "reserve-egress",
	classes: []defaultClassSpec{
		{name: "partner", ratePct: 40, ceilPct: 70, prio: 0},
		{name: "public", ratePct: 30, ceilPct: 70, prio: 5},
		{name: "reserve-egress", ratePct: 30, ceilFull: true, prio: 1},
	},
}

// ingressProfile: three ingress classes at publisher 80%, backfill-response
// 10%, reserve-ingress 10%; all ceil 100% of trunk.
var ingressProfile = defaultProfile{
	dir:          DirIngress,
	defaultClass: "reserve-ingress",
	classes: []defaultClassSpec{
		{name: "publisher", ratePct: 80, ceilFull: true, prio: 0},
		{name: "backfill-response", ratePct: 10, ceilFull: true, prio: 7},
		{name: "reserve-ingress", ratePct: 10, ceilFull: true, prio: 1},
	},
}

// buildDefaultConfig materialises a profile against a concrete trunkRate,
// returning the device root and per-class configs with rates/ceils computed
// from the trunk. All records share a single CreatedAt. Exposed as a
// package-internal helper so tests can verify the computation without disk I/O.
func buildDefaultConfig(p defaultProfile, trunkRate string) (*DeviceConfig, []*ClassConfig, error) {
	bps, err := parseBandwidthBps(trunkRate)
	if err != nil {
		return nil, nil, errorx.IllegalArgument.Wrap(err, "invalid trunk rate %q", trunkRate)
	}
	mbps := bps / 1_000_000
	now := time.Now().UTC()
	dev := &DeviceConfig{
		Dir:          p.dir,
		Rate:         trunkRate,
		DefaultClass: p.defaultClass,
		CreatedAt:    now,
	}
	classes := make([]*ClassConfig, 0, len(p.classes))
	for _, c := range p.classes {
		ceil := trunkRate
		if !c.ceilFull {
			ceil = fmt.Sprintf("%dmbit", mbps*int64(c.ceilPct)/100)
		}
		classes = append(classes, &ClassConfig{
			Name:      c.name,
			Rate:      fmt.Sprintf("%dmbit", mbps*int64(c.ratePct)/100),
			Ceil:      ceil,
			Prio:      c.prio,
			CreatedAt: now,
		})
	}
	return dev, classes, nil
}

// defaultEgressConfig returns the egress device root and three default egress
// classes at proportions derived from trunkRate (partner 40%/70%, public
// 30%/70%, reserve-egress 30%/100%).
func defaultEgressConfig(trunkRate string) (*DeviceConfig, []*ClassConfig, error) {
	return buildDefaultConfig(egressProfile, trunkRate)
}

// defaultIngressConfig returns the ingress device root and three default
// ingress classes at proportions derived from trunkRate (publisher 80%,
// backfill-response 10%, reserve-ingress 10%; all ceil 100% of trunk).
func defaultIngressConfig(trunkRate string) (*DeviceConfig, []*ClassConfig, error) {
	return buildDefaultConfig(ingressProfile, trunkRate)
}
