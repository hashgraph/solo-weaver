// SPDX-License-Identifier: Apache-2.0

package network

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/daemon/privexec"
)

const mib = uint64(1 << 20)

var t0 = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

// sample is a counter reading with $EGRESS (partner, reserve-egress default)
// and one pod veth (publisher, reserve-ingress default).
type sample struct {
	epoch                  string
	forward                uint64
	rules                  map[string]uint64
	egressLanes, vethLanes map[string]uint64
	egressTrunk, vethTrunk uint64
	vethIfIndex            int
	noVeth, uncounted      bool
}

func (s sample) build() *privexec.NetworkCounterSample {
	if s.epoch == "" {
		s.epoch = "7"
	}
	if s.vethIfIndex == 0 {
		s.vethIfIndex = 12
	}
	out := &privexec.NetworkCounterSample{
		RulesEpoch: s.epoch, ForwardCounted: !s.uncounted, ForwardBytes: s.forward, RuleBytes: s.rules,
		Devices: []privexec.NetworkDeviceCounters{{
			Dev: "eth0", Role: privexec.NetworkRoleEgress, IfIndex: 2, DefaultClass: "reserve-egress",
			TrunkBytes: s.egressTrunk, LaneBytes: s.egressLanes,
		}},
	}
	if !s.noVeth {
		out.Devices = append(out.Devices, privexec.NetworkDeviceCounters{
			Dev: "lxc1", Role: privexec.NetworkRoleVeth, IfIndex: s.vethIfIndex, DefaultClass: "reserve-ingress",
			TrunkBytes: s.vethTrunk, LaneBytes: s.vethLanes,
		})
	}
	return out
}

func okChecks(c *privexec.NetworkCounterSample) *privexec.NetworkChecks {
	return &privexec.NetworkChecks{
		Rules:    privexec.NetworkRulesCheck{Status: privexec.NetworkCheckOK},
		Filters:  privexec.NetworkFiltersCheck{Status: privexec.NetworkCheckOK},
		Counters: c,
	}
}

// grow returns s with every counter raised: rules by rule, lanes by lane.
func (s sample) grow(rule, lane map[string]uint64, forward, trunk uint64) sample {
	next := s
	next.rules = addAll(s.rules, rule)
	next.egressLanes = addAll(s.egressLanes, pick(lane, "partner", "public", "reserve-egress"))
	next.vethLanes = addAll(s.vethLanes, pick(lane, "publisher", "backfill-response", "reserve-ingress"))
	next.forward += forward
	next.egressTrunk += trunk
	next.vethTrunk += trunk
	return next
}

func addAll(base, delta map[string]uint64) map[string]uint64 {
	out := map[string]uint64{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range delta {
		out[k] += v
	}
	return out
}

func pick(m map[string]uint64, keys ...string) map[string]uint64 {
	out := map[string]uint64{}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

func baseline() sample {
	zero := map[string]uint64{"partner": 0, "reserve-egress": 0, "publisher": 0, "reserve-ingress": 0}
	return sample{
		rules:       map[string]uint64{"partner": 0, "publisher": 0, "reserve-egress": 0, "reserve-ingress": 0},
		egressLanes: pick(zero, "partner", "reserve-egress"),
		vethLanes:   pick(zero, "publisher", "reserve-ingress"),
	}
}

// run feeds samples a minute apart and returns the last events and the judge.
func run(t *testing.T, samples ...sample) (*judge, []event) {
	t.Helper()
	j := newJudge()
	var events []event
	for i, s := range samples {
		events = j.observe(okChecks(s.build()), t0.Add(time.Duration(i)*time.Minute))
	}
	return j, events
}

func laneStatus(t *testing.T, j *judge, cat string) LaneState {
	t.Helper()
	for _, l := range j.state.Lanes {
		if l.Category == cat {
			return l
		}
	}
	t.Fatalf("no lane state for %s in %+v", cat, j.state.Lanes)
	return LaneState{}
}

func reasons(events []event, kind eventKind) []string {
	var out []string
	for _, e := range events {
		if e.kind == kind {
			out = append(out, e.problem.Reason)
		}
	}
	return out
}

func TestJudge_ReadsEachCounterPairAsThePlanSays(t *testing.T) {
	b := baseline()
	next := b.grow(
		map[string]uint64{"partner": 5 * mib, "publisher": 5 * mib, "reserve-egress": 5 * mib},
		map[string]uint64{"partner": 5 * mib, "reserve-egress": 6 * mib, "reserve-ingress": 2 * mib},
		10*mib, 10*mib)

	j, events := run(t, b, next)

	assert.Equal(t, laneWorking, laneStatus(t, j, "partner").Status, "both went up")
	assert.Equal(t, laneNotVerified, laneStatus(t, j, "reserve-egress").Status, "a default lane is never proven")
	assert.Equal(t, laneNotVerified, laneStatus(t, j, "reserve-ingress").Status, "unstamped traffic fills a default lane")
	assert.Equal(t, laneBroken, laneStatus(t, j, "publisher").Status, "stamped a lot, lane got nothing")
	assert.Empty(t, reasons(events, eventRaised), "one broken minute is not yet a problem")
}

func TestJudge_BrokenLaneIsRaisedOnlyTwiceInARow(t *testing.T) {
	b := baseline()
	broken := func(s sample) sample {
		return s.grow(map[string]uint64{"publisher": 5 * mib}, nil, 5*mib, 5*mib)
	}
	m1 := broken(b)
	m2 := broken(m1)

	j, events := run(t, b, m1, m2)

	assert.Equal(t, []string{reasonLaneBroken}, reasons(events, eventRaised))
	require.Len(t, j.state.Problems, 1)
	p := j.state.Problems[0]
	assert.Equal(t, "publisher", p.Category)
	assert.Equal(t, "lxc1", p.Interface)
	assert.Equal(t, 2, p.Consecutive)
	assert.Contains(t, p.Hint, "tc -s class show dev lxc1")
	require.NotNil(t, j.lastGood)
	assert.Equal(t, t0, *j.lastGood, "only the minute before the first broken reading was good")
}

func TestJudge_OneOddMinuteBetweenGoodOnesRaisesNothing(t *testing.T) {
	b := baseline()
	m1 := b.grow(map[string]uint64{"publisher": 5 * mib}, nil, 5*mib, 0)
	m2 := m1.grow(map[string]uint64{"publisher": 5 * mib}, map[string]uint64{"publisher": 5 * mib}, 5*mib, 0)
	m3 := m2.grow(map[string]uint64{"publisher": 5 * mib}, nil, 5*mib, 0)

	_, events := run(t, b, m1, m2, m3)

	assert.Empty(t, reasons(events, eventRaised))
}

func TestJudge_SmallStampedTrafficIsNotJudged(t *testing.T) {
	b := baseline()
	next := b.grow(map[string]uint64{"partner": mib - 1}, nil, mib, 0)

	j, _ := run(t, b, next)

	assert.Equal(t, laneNotVerified, laneStatus(t, j, "partner").Status)
}

func TestJudge_UnstampedTrafficInANormalLaneIsSuspicious(t *testing.T) {
	b := baseline()
	fill := func(s sample) sample { return s.grow(nil, map[string]uint64{"partner": 3 * mib}, 3*mib, 3*mib) }
	m1 := fill(b)

	j, events := run(t, b, m1, fill(m1))

	assert.Equal(t, laneSuspicious, laneStatus(t, j, "partner").Status)
	assert.Equal(t, []string{reasonLaneSuspicious}, reasons(events, eventRaised))
}

func TestJudge_QuietLaneWarnsOnceAfterAnHour(t *testing.T) {
	samples := []sample{baseline()}
	for range 62 {
		samples = append(samples, samples[len(samples)-1].grow(nil, nil, mib, 0))
	}
	j := newJudge()
	idle := 0
	for i, s := range samples {
		idle += len(reasons(j.observe(okChecks(s.build()), t0.Add(time.Duration(i)*time.Minute)), eventIdle))
	}

	// Every quiet category warns once, never again while quiet.
	assert.Equal(t, 4, idle)
	l := laneStatus(t, j, "partner")
	assert.Equal(t, laneIdle, l.Status)
	require.NotNil(t, l.IdleSince)
	assert.Equal(t, t0.Add(time.Minute), *l.IdleSince)
	assert.Empty(t, j.state.Problems, "quiet is not a problem")
}

func TestJudge_ResetSkipsTheMinuteButKeepsTheStreak(t *testing.T) {
	b := baseline()
	broken := func(s sample) sample { return s.grow(map[string]uint64{"publisher": 5 * mib}, nil, 5*mib, 0) }

	for name, reset := range map[string]func(sample) sample{
		"rules reloaded": func(s sample) sample { s.epoch = "8"; return s },
		"pod restarted":  func(s sample) sample { s.vethIfIndex = 99; return s },
		"counter backwards": func(s sample) sample {
			s.rules = map[string]uint64{"publisher": 0}
			return s
		},
	} {
		t.Run(name, func(t *testing.T) {
			m1 := broken(b)
			m2 := reset(broken(m1))
			m3 := broken(m2)

			j := newJudge()
			var raised []string
			for i, s := range []sample{b, m1, m2, m3} {
				raised = append(raised, reasons(j.observe(okChecks(s.build()), t0.Add(time.Duration(i)*time.Minute)), eventRaised)...)
				if i == 2 {
					assert.Equal(t, laneNotVerified, j.state.Lanes[0].Status)
					assert.Equal(t, "*", j.state.Lanes[0].Category)
				}
			}
			assert.Equal(t, []string{reasonLaneBroken}, raised, "m1 and m3 are two judged minutes in a row")
		})
	}
}

// TestJudge_OlderFormatNeverJudgesLanes is the upgrade guard: a table without
// counters would otherwise read as "every busy lane is suspicious".
func TestJudge_OlderFormatNeverJudgesLanes(t *testing.T) {
	old := sample{uncounted: true, egressLanes: map[string]uint64{"partner": 0}}
	busy := old
	busy.egressLanes = map[string]uint64{"partner": 50 * mib}
	busier := busy
	busier.egressLanes = map[string]uint64{"partner": 100 * mib}

	j, events := run(t, old, busy, busier)

	assert.Empty(t, events)
	assert.Contains(t, j.state.Lanes[0].Detail, "older render format")
}

func TestJudge_BusyLanesWithFlatRulesAreBypassed(t *testing.T) {
	b := baseline()
	skip := func(s sample) sample {
		return s.grow(nil, map[string]uint64{"reserve-egress": 4 * mib, "reserve-ingress": 4 * mib}, 0, 4*mib)
	}
	m1 := skip(b)

	_, events := run(t, b, m1, skip(m1))

	assert.Equal(t, []string{reasonRulesBypassed}, reasons(events, eventRaised))
}

func TestJudge_BypassNeedsAPodVeth(t *testing.T) {
	b := baseline()
	b.noVeth = true
	skip := func(s sample) sample { return s.grow(nil, map[string]uint64{"reserve-egress": 4 * mib}, 0, 4*mib) }
	m1 := skip(b)

	_, events := run(t, b, m1, skip(m1))

	assert.NotContains(t, reasons(events, eventRaised), reasonRulesBypassed,
		"host traffic alone never passes the forward hook")
}

func TestJudge_HostTrafficOnTheNICIsNotABypass(t *testing.T) {
	b := baseline()
	hostOnly := func(s sample) sample {
		next := s
		next.egressLanes = addAll(s.egressLanes, map[string]uint64{"reserve-egress": 4 * mib})
		next.egressTrunk += 4 * mib
		return next
	}
	m1 := hostOnly(b)

	_, events := run(t, b, m1, hostOnly(m1))

	assert.NotContains(t, reasons(events, eventRaised), reasonRulesBypassed,
		"a quiet pod with a busy host must not read as a bypass")
}

func TestJudge_DriftIsRaisedAtOnceWithTheDifferingLines(t *testing.T) {
	j := newJudge()
	c := okChecks(nil)
	c.Rules = privexec.NetworkRulesCheck{Status: privexec.NetworkCheckFailed, Detail: "differ",
		Missing: []string{"rule a"}, Unexpected: []string{"rule b"}}

	events := j.observe(c, t0)

	require.Len(t, events, 1)
	assert.Equal(t, eventRaised, events[0].kind)
	assert.Equal(t, reasonRulesDrifted, events[0].problem.Reason)
	assert.Equal(t, []string{"- rule a", "+ rule b"}, events[0].problem.Lines)
	assert.Equal(t, "nft list table inet weaver-workload-policy", events[0].problem.Hint)
}

func TestJudge_LastingProblemRemindsHourlyThenClears(t *testing.T) {
	j := newJudge()
	drift := okChecks(nil)
	drift.Rules = privexec.NetworkRulesCheck{Status: privexec.NetworkCheckFailed}

	var raised, reminders int
	for i := 0; i <= 120; i++ {
		ev := j.observe(drift, t0.Add(time.Duration(i)*time.Minute))
		raised += len(reasons(ev, eventRaised))
		reminders += len(reasons(ev, eventReminder))
	}
	assert.Equal(t, 1, raised)
	assert.Equal(t, 2, reminders, "at +60m and +120m")
	assert.Equal(t, 121, j.state.Problems[0].Consecutive)
	assert.Equal(t, t0, j.state.Problems[0].Since)

	ev := j.observe(okChecks(nil), t0.Add(121*time.Minute))
	assert.Equal(t, []string{reasonRulesDrifted}, reasons(ev, eventCleared))
	assert.Empty(t, j.state.Problems)
	require.NotNil(t, j.lastGood)
	assert.Equal(t, t0.Add(121*time.Minute), *j.lastGood)
}

func TestJudge_UnknownDoesNotClearAnEarlierProblem(t *testing.T) {
	j := newJudge()
	drift := okChecks(nil)
	drift.Rules = privexec.NetworkRulesCheck{Status: privexec.NetworkCheckFailed}
	unknown := okChecks(nil)
	unknown.Rules = privexec.NetworkRulesCheck{Status: privexec.NetworkCheckUnknown, Detail: "cannot determine: x"}

	j.observe(drift, t0)
	ev := j.observe(unknown, t0.Add(time.Minute))

	assert.Empty(t, reasons(ev, eventCleared))
	assert.Equal(t, []string{reasonCheckUnknown}, reasons(ev, eventRaised))
	assert.Len(t, j.state.Problems, 2)
}

func TestJudge_NoPolicyTableClearsAnEarlierLaneProblem(t *testing.T) {
	b := baseline()
	broken := func(s sample) sample {
		return s.grow(map[string]uint64{"publisher": 5 * mib}, nil, 5*mib, 5*mib)
	}
	m1 := broken(b)
	j, _ := run(t, b, m1, broken(m1))
	require.Len(t, j.state.Problems, 1)

	gone := okChecks(nil)
	gone.Rules = privexec.NetworkRulesCheck{Status: privexec.NetworkCheckNotChecked}
	at := t0.Add(3 * time.Minute)
	ev := j.observe(gone, at)

	assert.Equal(t, []string{reasonLaneBroken}, reasons(ev, eventCleared))
	assert.Empty(t, j.state.Problems)
	require.NotNil(t, j.lastGood)
	assert.Equal(t, at, *j.lastGood)
}

func TestJudge_StrayFilterNamesItsDevice(t *testing.T) {
	j := newJudge()
	c := okChecks(nil)
	c.Filters = privexec.NetworkFiltersCheck{Status: privexec.NetworkCheckFailed, Devices: []privexec.NetworkDeviceFilters{
		{Dev: "eth0", Role: privexec.NetworkRoleEgress},
		{Dev: "lxc1", Role: privexec.NetworkRoleVeth, Filters: []string{"parent 1: pref 1 protocol all kind u32"}},
	}}

	events := j.observe(c, t0)

	require.Len(t, events, 1)
	p := events[0].problem
	assert.Equal(t, reasonStrayTcFilter, p.Reason)
	assert.Equal(t, "lxc1", p.Interface)
	assert.True(t, strings.HasPrefix(p.Hint, "tc filter show dev lxc1 parent 1:"))
}

func TestJudge_SkippedRunChangesNothing(t *testing.T) {
	j := newJudge()
	j.observe(okChecks(baseline().build()), t0)
	good := j.lastGood
	skipped := &privexec.NetworkChecks{
		Rules:   privexec.NetworkRulesCheck{Status: privexec.NetworkCheckSkipped},
		Filters: privexec.NetworkFiltersCheck{Status: privexec.NetworkCheckSkipped},
	}

	assert.Nil(t, j.observe(skipped, t0.Add(time.Minute)))
	assert.Equal(t, good, j.lastGood)
	assert.Equal(t, privexec.NetworkCheckOK, j.state.Rules)
}

// TestJudge_OlderFormatIsCalmAndStillGood: the worker only reports it when the
// rules otherwise match the registry, so every upgraded host is not flagged bad.
func TestJudge_OlderFormatIsCalmAndStillGood(t *testing.T) {
	j := newJudge()
	c := okChecks(nil)
	c.Rules.Status = privexec.NetworkCheckOlderFormat

	events := j.observe(c, t0)

	assert.Equal(t, []string{reasonRulesOlderFormat}, reasons(events, eventRaised))
	require.NotNil(t, j.lastGood)
}

func TestCategories_UnionOfRulesAndLanesSortedOnce(t *testing.T) {
	s := &privexec.NetworkCounterSample{
		RuleBytes: map[string]uint64{"publisher": 9, "partner": 4},
		Devices: []privexec.NetworkDeviceCounters{
			{Dev: "eth0", LaneBytes: map[string]uint64{"partner": 1, "reserve-egress": 2}},
			{Dev: "lxc1", LaneBytes: map[string]uint64{"publisher": 3, "backfill-response": 5}},
			{Dev: "lxc2", LaneBytes: map[string]uint64{"backfill-response": 6}},
		},
	}

	assert.Equal(t, []string{"backfill-response", "partner", "publisher", "reserve-egress"}, categories(s))
}

// categories writes lane-only classes into its own set; doing that on the
// sample's map would invent rules and zero the shared ones.
func TestCategories_LeavesTheSampleUntouched(t *testing.T) {
	rules := map[string]uint64{"publisher": 100 * mib, "partner": 7}
	s := &privexec.NetworkCounterSample{
		RuleBytes: rules,
		Devices: []privexec.NetworkDeviceCounters{
			{Dev: "lxc1", LaneBytes: map[string]uint64{"publisher": 1, "backfill-response": 2}},
		},
	}

	categories(s)

	assert.Equal(t, map[string]uint64{"publisher": 100 * mib, "partner": 7}, s.RuleBytes)
	assert.Equal(t, map[string]uint64{"publisher": 1, "backfill-response": 2}, s.Devices[0].LaneBytes)
}

func TestCategories_EdgeShapes(t *testing.T) {
	for name, tc := range map[string]struct {
		in   *privexec.NetworkCounterSample
		want []string
	}{
		"empty sample": {&privexec.NetworkCounterSample{}, nil},
		"nil rules, lanes only": {&privexec.NetworkCounterSample{Devices: []privexec.NetworkDeviceCounters{
			{LaneBytes: map[string]uint64{"z": 1, "a": 1}},
		}}, []string{"a", "z"}},
		"rules only, no devices": {&privexec.NetworkCounterSample{
			RuleBytes: map[string]uint64{"b": 0, "a": 0},
		}, []string{"a", "b"}},
		"device with nil lanes": {&privexec.NetworkCounterSample{
			RuleBytes: map[string]uint64{"a": 1},
			Devices:   []privexec.NetworkDeviceCounters{{Dev: "eth0"}},
		}, []string{"a"}},
		"empty non-nil maps": {&privexec.NetworkCounterSample{
			RuleBytes: map[string]uint64{},
			Devices:   []privexec.NetworkDeviceCounters{{LaneBytes: map[string]uint64{}}},
		}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			got := categories(tc.in)
			assert.Len(t, got, len(tc.want))
			if len(tc.want) > 0 {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

// The judge keeps each sample as the next minute's baseline, so a mutated
// rule counter would show up as a wrong delta one minute later.
func TestJudge_SteadyLaneStaysWorkingOverManyMinutes(t *testing.T) {
	b := baseline()
	b.rules["publisher"] = 100 * mib
	b.vethLanes["backfill-response"] = 0
	steady := func(s sample) sample {
		return s.grow(
			map[string]uint64{"publisher": 5 * mib},
			map[string]uint64{"publisher": 5 * mib, "backfill-response": 0},
			5*mib, 5*mib)
	}
	m1 := steady(b)
	m2 := steady(m1)
	m3 := steady(m2)

	j, events := run(t, b, m1, m2, m3)

	assert.Equal(t, laneWorking, laneStatus(t, j, "publisher").Status)
	assert.Equal(t, laneIdle, laneStatus(t, j, "backfill-response").Status)
	assert.Empty(t, reasons(events, eventRaised))
	assert.Equal(t, 115*mib, j.prev.RuleBytes["publisher"])
	assert.NotContains(t, j.prev.RuleBytes, "backfill-response", "a lane-only class must not become a rule")
}
