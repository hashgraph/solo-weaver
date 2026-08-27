// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"fmt"
	"strings"
	"time"

	"github.com/automa-saga/logx"
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

// mergeExistingConfig folds the registry's current records (existingDev,
// existingClasses — both nil on a fresh install) into the freshly computed
// dev/classes, in place, so a re-provision never discards operator state:
//
//   - CreatedAt is carried over for every record that already exists — a
//     re-provision updates the registry, it does not recreate it.
//   - The device's default class is operator-owned: a recorded one survives,
//     whatever the trunk rate does. One this provision no longer writes falls
//     back to the profile value with a warning — kept, it would either fail the
//     render or name a class the boot script never creates.
//   - When the requested trunk rate is the same bandwidth as the one already
//     recorded on the device, each class keeps its recorded rate/ceil/prio
//     instead of being reset to the profile proportions. This is what makes
//     `network shape set` tuning survive a bare `block node reconfigure` /
//     `upgrade`: both resolve the trunk rate back from persisted state, so the
//     rate always arrives non-empty and cannot itself signal "the operator
//     asked for new shaping on this run" (#1037).
//   - A class absent from the registry (e.g. one added in a later version)
//     keeps its computed default, so a re-provision still materialises it.
//
// A trunk rate that is a *different* bandwidth rebalances every class
// proportionally — the intentional `--link-rate` path. Per-class `--shape`
// overrides are merged on top of whatever base this leaves behind, so they win
// either way (see applyClassOverrides).
func mergeExistingConfig(dev *DeviceConfig, classes []*ClassConfig, existingDev *DeviceConfig, existingClasses []*ClassConfig) {
	if existingDev == nil {
		return
	}
	if !existingDev.CreatedAt.IsZero() {
		dev.CreatedAt = existingDev.CreatedAt
	}
	if existingDev.DefaultClass != "" {
		if hasClassNamed(classes, existingDev.DefaultClass) {
			dev.DefaultClass = existingDev.DefaultClass
		} else {
			logx.As().Warn().
				Str("dir", dev.Dir).
				Str("recorded", existingDev.DefaultClass).
				Str("fallback", dev.DefaultClass).
				Msg("recorded default class is not one of this device's classes; falling back to the profile default")
		}
	}
	keepClassRates := sameBandwidth(existingDev.Rate, dev.Rate)
	if keepClassRates {
		// Same bandwidth, possibly spelled differently (1gbit vs 1000mbit):
		// keep the recorded spelling so the rendered boot script stays
		// byte-identical and writeEgressScript can skip the write.
		dev.Rate = existingDev.Rate
	}

	byName := make(map[string]*ClassConfig, len(existingClasses))
	for _, c := range existingClasses {
		byName[c.Name] = c
	}
	for _, c := range classes {
		prev, ok := byName[c.Name]
		if !ok {
			continue
		}
		if !prev.CreatedAt.IsZero() {
			c.CreatedAt = prev.CreatedAt
		}
		if keepClassRates {
			c.Rate = prev.Rate
			c.Ceil = prev.Ceil
			c.Prio = prev.Prio
		}
	}

	logx.As().Debug().
		Str("dir", dev.Dir).
		Str("trunkRate", dev.Rate).
		Str("defaultClass", dev.DefaultClass).
		Bool("preservedClassRates", keepClassRates).
		Msg("folded existing shape registry records into re-provisioned defaults")
}

// loadExistingConfig reads the registry's device + class records for dir,
// returning (nil, nil, nil) when nothing has been provisioned yet. Split out of
// mergeExistingConfig so the merge itself stays disk-free and unit-testable.
func loadExistingConfig(dir string) (*DeviceConfig, []*ClassConfig, error) {
	dev, err := readDevice(dir)
	if err != nil {
		return nil, nil, err
	}
	if dev == nil {
		return nil, nil, nil
	}
	classes, err := loadClassesForDir(dir)
	if err != nil {
		return nil, nil, err
	}
	return dev, classes, nil
}

// hasClassNamed reports whether classes contains a class of that name. The merge
// gates the recorded default class on this rather than validateDefaultClass,
// which still accepts a class dropped from the direction's profile.
func hasClassNamed(classes []*ClassConfig, name string) bool {
	for _, c := range classes {
		if c.Name == name {
			return true
		}
	}
	return false
}

// sameBandwidth reports whether two tc-style rate strings denote the same
// bandwidth, so "1gbit" and "1000mbit" compare equal. Values that do not parse
// (e.g. a legacy shell expression in a hand-edited device file) are compared
// verbatim.
func sameBandwidth(a, b string) bool {
	aBps, aErr := parseBandwidthBps(a)
	bBps, bErr := parseBandwidthBps(b)
	if aErr != nil || bErr != nil {
		return strings.TrimSpace(a) == strings.TrimSpace(b)
	}
	return aBps == bBps
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
