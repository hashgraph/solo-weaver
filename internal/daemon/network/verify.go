// SPDX-License-Identifier: Apache-2.0

package network

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/privexec"
)

// Reason codes for the checks; a stable code per log line lets operators alert on it.
const (
	reasonRulesDrifted     = "NetworkRulesDrifted"
	reasonRulesOlderFormat = "NetworkRulesOlderFormat"
	reasonStrayTcFilter    = "NetworkStrayTcFilter"
	reasonLaneBroken       = "NetworkLaneBroken"
	reasonLaneSuspicious   = "NetworkLaneSuspicious"
	reasonRulesBypassed    = "NetworkRulesBypassed"
	reasonCheckUnknown     = "NetworkCheckUnknown"
	reasonLaneIdle         = "NetworkLaneIdle"
	reasonHealthyAgain     = "NetworkCheckHealthyAgain"
)

// Check names, as logged and reported by GET /status.
const (
	checkRules   = "rules"
	checkFilters = "filters"
	checkLanes   = "lanes"
)

// Lane statuses, as reported by GET /status.
const (
	laneWorking     = "working"
	laneNotVerified = "not-verified"
	laneIdle        = "idle"
	laneBroken      = "broken"
	laneSuspicious  = "suspicious"
)

// Vars, not consts, so tests can shrink them.
var (
	// minJudgeBytes: below this per-minute traffic a lane is too quiet to judge.
	minJudgeBytes uint64 = 1 << 20
	// laneConfirmRuns: one noisy minute must not raise a lane problem.
	laneConfirmRuns = 2
	// remindEvery keeps a lasting problem visible without flooding the log.
	remindEvery = time.Hour
	// idleWarnAfter: a lane idle this long may mean its traffic is routed elsewhere.
	idleWarnAfter = time.Hour
)

// Problem is one confirmed finding, as reported by GET /status.
type Problem struct {
	Reason    string `json:"reason"`
	Check     string `json:"check"`
	Interface string `json:"interface,omitempty"`
	Category  string `json:"category,omitempty"`
	Detail    string `json:"detail"`
	// Lines are the differing rule lines: "- " expected, "+ " live.
	Lines []string `json:"lines,omitempty"`
	// Hint is a command an operator can run to see the problem.
	Hint        string    `json:"hint"`
	Consecutive int       `json:"consecutive"`
	Since       time.Time `json:"since"`
}

// LaneState is one traffic category's latest reading.
type LaneState struct {
	Category  string `json:"category"`
	Interface string `json:"interface,omitempty"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	// IdleSince is when the lane last carried traffic, while it carries none.
	IdleSince *time.Time `json:"idle_since,omitempty"`
}

// ChecksState is the correctness checks' state, as reported by GET /status.
type ChecksState struct {
	Rules         string      `json:"rules"`
	RulesDetail   string      `json:"rules_detail,omitempty"`
	Filters       string      `json:"filters"`
	FiltersDetail string      `json:"filters_detail,omitempty"`
	Lanes         []LaneState `json:"lanes,omitempty"`
	Problems      []Problem   `json:"problems,omitempty"`
}

// eventKind is what a log line is about.
type eventKind int

const (
	eventRaised eventKind = iota
	eventReminder
	eventCleared
	eventIdle
)

// event is one line the judge asks the monitor to log.
type event struct {
	kind    eventKind
	problem Problem
}

// finding is one observation this run, before it is confirmed.
type finding struct {
	Problem
	// confirmRuns is how many runs in a row it must be seen to become a Problem.
	confirmRuns int
}

// findingKey identifies a finding across runs.
type findingKey struct{ reason, check, iface, category string }

func keyOf(p Problem) findingKey { return findingKey{p.Reason, p.Check, p.Interface, p.Category} }

// active is a confirmed problem and when it was last logged.
type active struct {
	Problem
	lastLogged time.Time
}

// idleLane is when a lane last carried traffic, and whether that was warned about.
type idleLane struct {
	since  time.Time
	warned bool
}

// judge holds the history the stateless worker cannot (last sample, finding streaks).
// No I/O, so tests drive it directly.
type judge struct {
	prev     *privexec.NetworkCounterSample
	pending  map[findingKey]int
	active   map[findingKey]*active
	idle     map[string]*idleLane
	lastGood *time.Time
	state    *ChecksState
}

func newJudge() *judge {
	return &judge{
		pending: map[findingKey]int{},
		active:  map[findingKey]*active{},
		idle:    map[string]*idleLane{},
	}
}

// observe returns what to log for this run. A run that checked nothing (lock held)
// must not change history.
func (j *judge) observe(c *privexec.NetworkChecks, now time.Time) []event {
	if c == nil || (c.Rules.Status == privexec.NetworkCheckSkipped && c.Filters.Status == privexec.NetworkCheckSkipped) {
		return nil
	}

	findings := append(rulesFindings(c.Rules), filtersFindings(c.Filters)...)
	lanes, laneFindings, lanesJudged := j.judgeLanes(c.Counters)
	findings = append(findings, laneFindings...)
	// With no policy table there are no lanes to verify, so earlier lane problems clear.
	if c.Rules.Status == privexec.NetworkCheckNotChecked {
		lanesJudged = true
	}

	// An unknown result proves nothing, so it must not clear an earlier problem.
	judged := map[string]bool{
		checkRules:   c.Rules.Status != privexec.NetworkCheckUnknown,
		checkFilters: c.Filters.Status != privexec.NetworkCheckUnknown,
		checkLanes:   lanesJudged,
	}
	events := j.fold(findings, judged, now)
	events = append(events, j.trackIdle(lanes, lanesJudged, now)...)

	j.state = &ChecksState{
		Rules:         c.Rules.Status,
		RulesDetail:   c.Rules.Detail,
		Filters:       c.Filters.Status,
		FiltersDetail: c.Filters.Detail,
		Lanes:         lanes,
		Problems:      j.problems(),
	}
	if j.allGood(c, findings) {
		at := now
		j.lastGood = &at
	}
	return events
}

// fold keeps checks that were not judged this run untouched, so a skipped check
// neither confirms nor clears anything.
func (j *judge) fold(findings []finding, judged map[string]bool, now time.Time) []event {
	var events []event
	seen := map[findingKey]bool{}
	for _, f := range findings {
		k := keyOf(f.Problem)
		seen[k] = true
		j.pending[k]++
		n := j.pending[k]
		if n < f.confirmRuns {
			continue
		}
		f.Consecutive = n
		if a, ok := j.active[k]; ok {
			f.Since = a.Since
			a.Problem = f.Problem
			if now.Sub(a.lastLogged) >= remindEvery {
				a.lastLogged = now
				events = append(events, event{kind: eventReminder, problem: a.Problem})
			}
			continue
		}
		f.Since = now
		j.active[k] = &active{Problem: f.Problem, lastLogged: now}
		events = append(events, event{kind: eventRaised, problem: f.Problem})
	}

	for k, a := range j.active {
		if seen[k] || !judged[a.Check] {
			continue
		}
		delete(j.active, k)
		events = append(events, event{kind: eventCleared, problem: a.Problem})
	}
	for k := range j.pending {
		if !seen[k] && judged[k.check] {
			delete(j.pending, k)
		}
	}
	sortEvents(events)
	return events
}

// trackIdle notes lanes with no traffic, warning once when one stays idle.
func (j *judge) trackIdle(lanes []LaneState, judged bool, now time.Time) []event {
	if !judged {
		return nil
	}
	var events []event
	for i := range lanes {
		l := &lanes[i]
		if l.Status != laneIdle {
			delete(j.idle, l.Category)
			continue
		}
		il, ok := j.idle[l.Category]
		if !ok {
			il = &idleLane{since: now}
			j.idle[l.Category] = il
		}
		at := il.since
		l.IdleSince = &at
		if now.Sub(il.since) >= idleWarnAfter && !il.warned {
			il.warned = true
			events = append(events, event{kind: eventIdle, problem: Problem{
				Reason: reasonLaneIdle, Check: checkLanes, Interface: l.Interface, Category: l.Category,
				Detail: "no traffic in this category for over " + idleWarnAfter.String() + ", so its lane is not verified",
				Hint:   "tc -s class show dev " + firstDev(l.Interface), Since: il.since,
			}})
		}
	}
	return events
}

// allGood is false while any finding is pending, so "last good" is never a guess.
// Older-format rules still matched the registry, so they count as good.
func (j *judge) allGood(c *privexec.NetworkChecks, findings []finding) bool {
	fine := func(s string) bool {
		return s == privexec.NetworkCheckOK || s == privexec.NetworkCheckNotChecked || s == privexec.NetworkCheckOlderFormat
	}
	ranAny := c.Rules.Status == privexec.NetworkCheckOK || c.Rules.Status == privexec.NetworkCheckOlderFormat ||
		c.Filters.Status == privexec.NetworkCheckOK
	if !ranAny || !fine(c.Rules.Status) || !fine(c.Filters.Status) {
		return false
	}
	for _, f := range findings {
		if f.Reason != reasonRulesOlderFormat {
			return false
		}
	}
	for _, a := range j.active {
		if a.Reason != reasonRulesOlderFormat {
			return false
		}
	}
	return true
}

// problems returns the confirmed problems in a stable order.
func (j *judge) problems() []Problem {
	out := make([]Problem, 0, len(j.active))
	for _, a := range j.active {
		out = append(out, a.Problem)
	}
	slices.SortFunc(out, compareProblems)
	return out
}

func compareProblems(a, b Problem) int {
	return cmp.Or(
		strings.Compare(a.Reason, b.Reason),
		strings.Compare(a.Check, b.Check),
		strings.Compare(a.Interface, b.Interface),
		strings.Compare(a.Category, b.Category),
	)
}

func sortEvents(events []event) {
	slices.SortStableFunc(events, func(a, b event) int { return compareProblems(a.problem, b.problem) })
}

// rulesFindings confirms drift at once: the worker compared under the lock, so
// one reading is already certain.
func rulesFindings(rc privexec.NetworkRulesCheck) []finding {
	const hint = "nft list table inet weaver-workload-policy"
	switch rc.Status {
	case privexec.NetworkCheckFailed:
		lines := make([]string, 0, len(rc.Missing)+len(rc.Unexpected))
		for _, l := range rc.Missing {
			lines = append(lines, "- "+l)
		}
		for _, l := range rc.Unexpected {
			lines = append(lines, "+ "+l)
		}
		return []finding{{confirmRuns: 1, Problem: Problem{
			Reason: reasonRulesDrifted, Check: checkRules, Detail: rc.Detail, Lines: lines, Hint: hint,
		}}}
	case privexec.NetworkCheckOlderFormat:
		return []finding{{confirmRuns: 1, Problem: Problem{
			Reason: reasonRulesOlderFormat, Check: checkRules, Detail: rc.Detail, Hint: hint,
		}}}
	case privexec.NetworkCheckUnknown:
		return []finding{{confirmRuns: 1, Problem: Problem{
			Reason: reasonCheckUnknown, Check: checkRules, Detail: rc.Detail,
			Hint: "sudo solo-provisioner network reassert --check",
		}}}
	}
	return nil
}

// filtersFindings turns the filters check into one finding per stray device.
func filtersFindings(fc privexec.NetworkFiltersCheck) []finding {
	var out []finding
	for _, d := range fc.Devices {
		if len(d.Filters) == 0 {
			continue
		}
		out = append(out, finding{confirmRuns: 1, Problem: Problem{
			Reason: reasonStrayTcFilter, Check: checkFilters, Interface: d.Dev,
			Detail: "weaver installs no tc filters, but found: " + strings.Join(d.Filters, "; ") +
				" (it can move or drop traffic no rule stamped)",
			Hint: "tc filter show dev " + d.Dev + " parent 1: ; tc filter show dev " + d.Dev + " parent 1:1",
		}})
	}
	if fc.Status == privexec.NetworkCheckUnknown {
		out = append(out, finding{confirmRuns: 1, Problem: Problem{
			Reason: reasonCheckUnknown, Check: checkFilters, Detail: fc.Detail,
			Hint: "sudo solo-provisioner network reassert --check",
		}})
	}
	return out
}

// judgeLanes diffs two samples; judged is false when they cannot be compared
// (first sample, counter reset, no counters).
func (j *judge) judgeLanes(cur *privexec.NetworkCounterSample) (lanes []LaneState, findings []finding, judged bool) {
	prev := j.prev
	j.prev = cur
	if cur == nil {
		return nil, nil, false
	}
	if !cur.ForwardCounted {
		return []LaneState{{Category: "*", Status: laneNotVerified,
			Detail: "the live rules carry no counters yet (older render format)"}}, nil, false
	}
	if reason := resetReason(prev, cur); reason != "" {
		return []LaneState{{Category: "*", Status: laneNotVerified, Detail: reason}}, nil, false
	}

	for _, cat := range categories(cur) {
		r := readLane(prev, cur, cat)
		l, f := classifyLane(r)
		lanes = append(lanes, l)
		if f != nil {
			findings = append(findings, *f)
		}
	}
	if f := bypassFinding(prev, cur); f != nil {
		findings = append(findings, *f)
	}
	return lanes, findings, true
}

// resetReason explains why two samples cannot be compared, "" when they can.
func resetReason(prev, cur *privexec.NetworkCounterSample) string {
	switch {
	case prev == nil:
		return "first reading; the next one is compared with it"
	case !prev.ForwardCounted:
		return "first reading with counters; the next one is compared with it"
	case prev.RulesEpoch != cur.RulesEpoch:
		return "the rules were reloaded, so their counters restarted"
	case cur.ForwardBytes < prev.ForwardBytes:
		return "the rule counters went backwards"
	}
	for class, b := range cur.RuleBytes {
		if b < prev.RuleBytes[class] {
			return "the rule counters went backwards"
		}
	}
	if len(prev.Devices) != len(cur.Devices) {
		return "the set of lane trees changed (a pod may have restarted)"
	}
	for _, d := range cur.Devices {
		p, ok := deviceByName(prev, d.Dev)
		switch {
		case !ok || p.IfIndex != d.IfIndex:
			return "the lane tree on " + d.Dev + " was recreated"
		case d.Error != "" || p.Error != "":
			return "the lane counters on " + d.Dev + " could not be read"
		case d.TrunkBytes < p.TrunkBytes:
			return "the lane counters on " + d.Dev + " went backwards"
		}
		for class, b := range d.LaneBytes {
			if b < p.LaneBytes[class] {
				return "the lane counters on " + d.Dev + " went backwards"
			}
		}
	}
	return ""
}

func deviceByName(s *privexec.NetworkCounterSample, dev string) (privexec.NetworkDeviceCounters, bool) {
	for _, d := range s.Devices {
		if d.Dev == dev {
			return d, true
		}
	}
	return privexec.NetworkDeviceCounters{}, false
}

// categories lists every class a rule stamps or a lane tree carries.
func categories(s *privexec.NetworkCounterSample) []string {
	set := maps.Clone(s.RuleBytes)
	if set == nil {
		set = map[string]uint64{}
	}
	for _, d := range s.Devices {
		for c := range d.LaneBytes {
			set[c] = 0
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// laneReading is one category's traffic over the last minute.
type laneReading struct {
	category             string
	ruleDelta, laneDelta uint64
	hasRule, hasLane     bool
	isDefault            bool
	devs                 []string
}

func readLane(prev, cur *privexec.NetworkCounterSample, cat string) laneReading {
	r := laneReading{category: cat}
	if b, ok := cur.RuleBytes[cat]; ok {
		r.hasRule = true
		r.ruleDelta = b - prev.RuleBytes[cat]
	}
	for _, d := range cur.Devices {
		b, ok := d.LaneBytes[cat]
		if !ok {
			continue
		}
		p, _ := deviceByName(prev, d.Dev)
		r.hasLane = true
		r.laneDelta += b - p.LaneBytes[cat]
		r.devs = append(r.devs, d.Dev)
		r.isDefault = r.isDefault || d.DefaultClass == cat
	}
	return r
}

// classifyLane: only "broken" and "suspicious" are findings, and each must hold
// laneConfirmRuns minutes to avoid flapping.
func classifyLane(r laneReading) (LaneState, *finding) {
	dev := strings.Join(r.devs, ",")
	hintDev := firstDev(dev)
	l := LaneState{Category: r.category, Interface: dev}
	switch {
	case !r.hasLane:
		l.Status, l.Detail = laneNotVerified, "no lane tree carries this category right now"
	case r.hasRule && r.ruleDelta >= minJudgeBytes && r.laneDelta == 0:
		l.Status = laneBroken
		l.Detail = "the rules stamped " + bytesText(r.ruleDelta) + " for this category but its lane on " + dev + " received nothing"
		return l, &finding{confirmRuns: laneConfirmRuns, Problem: Problem{
			Reason: reasonLaneBroken, Check: checkLanes, Interface: dev, Category: r.category, Detail: l.Detail,
			Hint: "tc -s class show dev " + hintDev + " ; nft list table inet weaver-workload-policy ; cilium status | grep BandwidthManager",
		}}
	case r.ruleDelta > 0 && r.laneDelta > 0 && r.isDefault:
		l.Status, l.Detail = laneNotVerified, "this is the default lane, so its counter cannot tell stamped traffic from unstamped"
	case r.ruleDelta > 0 && r.laneDelta > 0:
		l.Status = laneWorking
	case r.ruleDelta == 0 && r.laneDelta >= minJudgeBytes && !r.isDefault:
		l.Status = laneSuspicious
		l.Detail = "the lane on " + dev + " received " + bytesText(r.laneDelta) + " but no rule stamped this category; something else is filling it"
		return l, &finding{confirmRuns: laneConfirmRuns, Problem: Problem{
			Reason: reasonLaneSuspicious, Check: checkLanes, Interface: dev, Category: r.category, Detail: l.Detail,
			Hint: "tc filter show dev " + hintDev + " parent 1: ; tc -s class show dev " + hintDev,
		}}
	case r.ruleDelta == 0 && r.laneDelta == 0:
		l.Status, l.Detail = laneIdle, "no traffic in this category"
	default:
		l.Status, l.Detail = laneNotVerified, "too little traffic to judge"
	}
	return l, nil
}

// bypassFinding catches traffic skipping the rules table (e.g. Cilium host routing
// changed). Pod veths only: host traffic on the NIC never hits the rules.
func bypassFinding(prev, cur *privexec.NetworkCounterSample) *finding {
	var trunk uint64
	for _, d := range cur.Devices {
		if d.Role != privexec.NetworkRoleVeth {
			continue
		}
		p, _ := deviceByName(prev, d.Dev)
		trunk += d.TrunkBytes - p.TrunkBytes
	}
	if trunk < minJudgeBytes || cur.ForwardBytes != prev.ForwardBytes {
		return nil
	}
	return &finding{confirmRuns: laneConfirmRuns, Problem: Problem{
		Reason: reasonRulesBypassed, Check: checkLanes,
		Detail: "the lanes carried " + bytesText(trunk) + " but weaver's rules saw no traffic; traffic is skipping them",
		Hint:   "kubectl -n kube-system exec ds/cilium -- cilium status --verbose | grep -iE 'Host Routing|BandwidthManager'",
	}}
}

// firstDev returns the first of a comma-joined device list, for a hint.
func firstDev(devs string) string {
	if devs == "" {
		return "<dev>"
	}
	first, _, _ := strings.Cut(devs, ",")
	return first
}

// bytesText renders a byte count for a log line.
func bytesText(b uint64) string {
	const mib = 1 << 20
	if b >= mib {
		return strconv.FormatUint(b/mib, 10) + " MiB"
	}
	return strconv.FormatUint(b, 10) + " B"
}
