// SPDX-License-Identifier: Apache-2.0

package reassert

import (
	"context"
	"net"
	"strconv"

	"github.com/hashgraph/solo-weaver/internal/network/nftexec"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
)

// Check outcomes. Part of the JSON contract with the daemon.
const (
	// CheckOK means the check ran and found nothing wrong.
	CheckOK = "ok"
	// CheckFailed means the check found a problem: drifted rules or a stray filter.
	CheckFailed = "failed"
	// CheckOlderFormat means the live rules were written by another render format.
	CheckOlderFormat = "older-format"
	// CheckUnknown means the check could not run; never read it as a failure.
	CheckUnknown = "unknown"
	// CheckNotChecked means there was nothing to check on this host this run.
	CheckNotChecked = "not-checked"
	// CheckSkipped means an operator apply held a lock, so nothing was looked at.
	CheckSkipped = "skipped"
)

// Device roles. Part of the JSON contract with the daemon.
const (
	RoleEgress = "egress"
	RoleVeth   = "veth"
	// RoleUnknown is a lane tree on a host whose shaped NIC could not be named.
	RoleUnknown = "unknown"
)

// maxDiffLines caps the differing lines a rules check reports.
const maxDiffLines = 20

// Checks is what one run found about whether weaver's traffic rules are right.
// The worker keeps no history: Counters are raw, and the daemon compares them.
type Checks struct {
	Rules   RulesCheck   `json:"rules"`
	Filters FiltersCheck `json:"filters"`
	// Counters is nil when the live rules could not be read.
	Counters *CounterSample `json:"counters,omitempty"`
}

// RulesCheck compares the live nft table with the one the registry calls for.
type RulesCheck struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	// LiveFormat is the render format of the live table, 0 when unread.
	LiveFormat int `json:"live_format,omitempty"`
	// ReloadedFrom is the older render format this run reloaded, 0 when none.
	ReloadedFrom int `json:"reloaded_from,omitempty"`
	// Missing are expected lines the live table lacks; Unexpected the reverse.
	Missing    []string `json:"missing,omitempty"`
	Unexpected []string `json:"unexpected,omitempty"`
}

// FiltersCheck looks for tc filters at the points HTB consults.
type FiltersCheck struct {
	Status  string          `json:"status"`
	Detail  string          `json:"detail,omitempty"`
	Devices []DeviceFilters `json:"devices,omitempty"`
}

// DeviceFilters is one lane tree's filters. Any filter is stray: weaver installs none.
type DeviceFilters struct {
	Dev     string   `json:"dev"`
	Role    string   `json:"role"`
	Filters []string `json:"filters,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// CounterSample is one reading of the rule and lane byte counters.
type CounterSample struct {
	// RulesEpoch changes whenever the table is reloaded and its counters reset.
	RulesEpoch string `json:"rules_epoch"`
	// ForwardCounted is false on a table rendered before the counters existed.
	ForwardCounted bool   `json:"forward_counted"`
	ForwardBytes   uint64 `json:"forward_bytes"`
	// RuleBytes is the stamp rules' byte count per class.
	RuleBytes map[string]uint64 `json:"rule_bytes"`
	Devices   []DeviceCounters  `json:"devices,omitempty"`
}

// DeviceCounters is one lane tree's class byte counters.
type DeviceCounters struct {
	Dev  string `json:"dev"`
	Role string `json:"role"`
	// IfIndex changes when the device is recreated, e.g. on a pod restart.
	IfIndex      int               `json:"ifindex"`
	DefaultClass string            `json:"default_class,omitempty"`
	TrunkBytes   uint64            `json:"trunk_bytes"`
	LaneBytes    map[string]uint64 `json:"lane_bytes"`
	Error        string            `json:"error,omitempty"`
}

// verifyScope is what the probes established this run.
type verifyScope struct {
	// policyLive is true when the workload-policy table is provisioned and present.
	policyLive bool
	// egressNIC is the shaped NIC when its lane tree is present, else "".
	egressNIC string
	// shapedNIC is the NIC the shaper script names, even when its probe failed.
	shapedNIC string
	// nicUnknown is set when shaping is provisioned but the NIC could not be named.
	nicUnknown bool
	// refreshOlderFormat lets the rules check reload a table an older render
	// format wrote. Only a full run sets it; --check never writes.
	refreshOlderFormat bool
}

// skippedChecks is the result of a run that held no locks.
func skippedChecks(detail string) *Checks {
	return &Checks{
		Rules:   RulesCheck{Status: CheckSkipped, Detail: detail},
		Filters: FiltersCheck{Status: CheckSkipped, Detail: detail},
	}
}

// verifier runs the checks. Every kernel read is a seam so tests need no kernel.
type verifier struct {
	expectedRules func(ctx context.Context) (string, error)
	liveRules     func(ctx context.Context) ([]byte, error)
	scratchRules  func(ctx context.Context, doc string) ([]byte, error)
	reloadRules   func(ctx context.Context, doc string) error
	canPersist    func() error
	laneTrees     func(ctx context.Context) ([]shape.LaneTree, error)
	treeFilters   func(ctx context.Context, dev string) ([]string, error)
	classStats    func(ctx context.Context, dev string) (map[string]shape.ClassStat, error)
	ifIndex       func(dev string) (int, error)
}

// newKernelVerifier returns a verifier wired to the live kernel.
func newKernelVerifier() *verifier { return kernelVerifierFor(policy.NewManager()) }

// kernelVerifierFor is newKernelVerifier with the policy registry and artifact mgr uses.
func kernelVerifierFor(mgr *policy.Manager) *verifier {
	bin, _ := nftexec.Binary()
	return &verifier{
		expectedRules: mgr.ExpectedDocument,
		liveRules:     func(ctx context.Context) ([]byte, error) { return policy.ListTableJSON(ctx, bin) },
		scratchRules: func(ctx context.Context, doc string) ([]byte, error) {
			return policy.ListInScratchNetns(ctx, bin, doc)
		},
		reloadRules: mgr.ReloadTable,
		canPersist:  mgr.CanPersist,
		laneTrees:   shape.LaneTrees,
		treeFilters: shape.TreeFilters,
		classStats:  shape.DeviceClassStats,
		ifIndex: func(dev string) (int, error) {
			iface, err := net.InterfaceByName(dev)
			if err != nil {
				return 0, err
			}
			return iface.Index, nil
		},
	}
}

// verify runs all three checks. The caller holds both plane locks.
func (v *verifier) verify(ctx context.Context, scope verifyScope) *Checks {
	out := &Checks{}
	live := v.checkRules(ctx, scope, &out.Rules)

	trees, treesErr := v.laneTreesFor(ctx, scope)
	out.Filters = v.checkFilters(ctx, scope, trees, treesErr)
	if live != nil {
		out.Counters = v.sampleCounters(ctx, scope, live, trees)
	}
	return out
}

// checkRules fills rc and returns the live listing, nil when it was not read.
func (v *verifier) checkRules(ctx context.Context, scope verifyScope, rc *RulesCheck) *policy.Listing {
	if !scope.policyLive {
		*rc = RulesCheck{Status: CheckNotChecked, Detail: "the workload-policy table is not live on this host"}
		return nil
	}
	unknown := func(what string, err error) {
		*rc = RulesCheck{Status: CheckUnknown, Detail: "cannot determine: " + what + ": " + err.Error()}
	}

	live, err := v.readLive(ctx)
	if err != nil {
		unknown("reading the live rules failed", err)
		return nil
	}

	// The listing is usable for counters even if the comparison fails.
	doc, err := v.expectedRules(ctx)
	if err != nil {
		unknown("building the expected rules from the registry failed", err)
		return live
	}
	expected, err := v.printExpected(ctx, doc)
	if err != nil {
		unknown("printing the expected rules failed", err)
		return live
	}

	*rc = compareRules(expected, live)
	// A newer format belongs to a newer binary, so only older ones are reloaded.
	if !scope.refreshOlderFormat || rc.Status != CheckOlderFormat || live.FormatVersion >= policy.FormatVersion {
		return live
	}
	if err := v.canPersist(); err != nil {
		// An unsaved reload reads back ok now but reverts on reboot.
		rc.Detail = formatDetail(live.FormatVersion) + "; not reloaded because the boot copy cannot be saved here (" +
			err.Error() + "); run `sudo solo-provisioner network reassert` to reload them"
		return live
	}
	return v.refreshRules(ctx, doc, expected, live.FormatVersion, rc)
}

// refreshRules reloads an older-format table with doc, the document just
// compared, then reads it back so the result describes the kernel, not the attempt.
func (v *verifier) refreshRules(ctx context.Context, doc string, expected *policy.Listing, from int, rc *RulesCheck) *policy.Listing {
	reloadErr := v.reloadRules(ctx, doc)

	live, err := v.readLive(ctx)
	if err != nil {
		*rc = RulesCheck{Status: CheckUnknown, Detail: "cannot determine: reading the rules back after reloading them failed: " + err.Error()}
		return nil
	}

	*rc = compareRules(expected, live)
	switch {
	case reloadErr != nil:
		// The kernel has the new format but the boot copy is still old, so a reboot would revert it.
		if rc.Status == CheckOK {
			rc.Status = CheckOlderFormat
		}
		rc.Detail = withDetail(rc.Detail, "reloading them in render format "+strconv.Itoa(policy.FormatVersion)+": "+reloadErr.Error())
	case rc.Status == CheckOK:
		rc.ReloadedFrom = from
		rc.Detail = "reloaded the live rules from render format " + strconv.Itoa(from) + " to " + strconv.Itoa(policy.FormatVersion)
	}
	return live
}

// readLive reads and parses the live table.
func (v *verifier) readLive(ctx context.Context) (*policy.Listing, error) {
	raw, err := v.liveRules(ctx)
	if err != nil {
		return nil, err
	}
	return policy.ParseListing(raw)
}

// printExpected loads doc into a scratch namespace and parses nft's listing of it.
func (v *verifier) printExpected(ctx context.Context, doc string) (*policy.Listing, error) {
	raw, err := v.scratchRules(ctx, doc)
	if err != nil {
		return nil, err
	}
	return policy.ParseListing(raw)
}

// compareRules judges the live listing against the expected one.
func compareRules(expected, live *policy.Listing) RulesCheck {
	rc := RulesCheck{Status: CheckOK, LiveFormat: live.FormatVersion}
	diff := policy.Compare(expected, live)
	if live.FormatVersion != policy.FormatVersion {
		// A format difference is expected; any other difference is drift.
		diff = policy.CompareIgnoringFormat(expected, live)
		if diff.Empty() {
			rc.Status = CheckOlderFormat
			rc.Detail = olderFormatDetail(live.FormatVersion)
			return rc
		}
	}
	switch {
	case diff.Empty():
	case diff.Reordered:
		rc.Status = CheckFailed
		rc.Detail = "the live rules match the registry but are in a different order"
	default:
		rc.Status = CheckFailed
		rc.Detail = "the live rules differ from the policy registry"
		rc.Missing = capLines(diff.Missing)
		rc.Unexpected = capLines(diff.Unexpected)
	}
	return rc
}

// formatDetail says which render formats differ.
func formatDetail(live int) string {
	return "the live rules use render format " + strconv.Itoa(live) +
		", this version renders format " + strconv.Itoa(policy.FormatVersion)
}

// olderFormatDetail says which formats differ and what brings the live rules current.
func olderFormatDetail(live int) string {
	d := formatDetail(live)
	if live < policy.FormatVersion {
		return d + "; the next `network reassert` run without --check reloads them (the daemon runs one every minute)"
	}
	return d + "; they are refreshed by the next `network policy create`/`delete` or a reboot"
}

// laneTreesFor lists the lane trees when shaping is live, or returns none.
func (v *verifier) laneTreesFor(ctx context.Context, scope verifyScope) ([]shape.LaneTree, error) {
	if scope.egressNIC == "" && !scope.policyLive {
		return nil, nil
	}
	return v.laneTrees(ctx)
}

// checkFilters looks for tc filters on every lane tree.
func (v *verifier) checkFilters(ctx context.Context, scope verifyScope, trees []shape.LaneTree, treesErr error) FiltersCheck {
	if treesErr != nil {
		return FiltersCheck{Status: CheckUnknown, Detail: "cannot determine: listing the lane trees failed: " + treesErr.Error()}
	}
	if len(trees) == 0 {
		return FiltersCheck{Status: CheckNotChecked, Detail: "no device carries weaver's lane tree"}
	}

	fc := FiltersCheck{Status: CheckOK}
	sawVeth, failed := false, false
	for _, t := range trees {
		d := DeviceFilters{Dev: t.Dev, Role: roleOf(t.Dev, scope)}
		sawVeth = sawVeth || d.Role == RoleVeth
		filters, err := v.treeFilters(ctx, t.Dev)
		switch {
		case err != nil:
			d.Error = err.Error()
			failed = true
		case len(filters) > 0:
			d.Filters = filters
			fc.Status = CheckFailed
		}
		fc.Devices = append(fc.Devices, d)
	}
	if failed && fc.Status == CheckOK {
		fc.Status = CheckUnknown
		fc.Detail = "cannot determine: reading the filters of a lane tree failed"
	}
	if !sawVeth && !scope.nicUnknown && fc.Detail == "" {
		fc.Detail = "no pod veth carries weaver's lane tree (the pod may be restarting); it was not checked"
	}
	return fc
}

// sampleCounters reads the rule counters from live and the lane counters from
// every lane tree.
func (v *verifier) sampleCounters(ctx context.Context, scope verifyScope, live *policy.Listing, trees []shape.LaneTree) *CounterSample {
	s := &CounterSample{
		RulesEpoch:     strconv.FormatInt(live.TableHandle, 10),
		ForwardCounted: live.ForwardCounted,
		ForwardBytes:   live.ForwardBytes,
		RuleBytes:      live.ClassBytes,
	}
	for _, t := range trees {
		s.Devices = append(s.Devices, v.deviceCounters(ctx, t, scope))
	}
	return s
}

// deviceCounters reads one lane tree's class counters; a read failure lands in Error.
func (v *verifier) deviceCounters(ctx context.Context, t shape.LaneTree, scope verifyScope) DeviceCounters {
	d := DeviceCounters{Dev: t.Dev, Role: roleOf(t.Dev, scope), DefaultClass: t.DefaultClass}
	idx, err := v.ifIndex(t.Dev)
	if err != nil {
		d.Error = err.Error()
		return d
	}
	d.IfIndex = idx
	stats, err := v.classStats(ctx, t.Dev)
	if err != nil {
		d.Error = err.Error()
		return d
	}
	d.LaneBytes = map[string]uint64{}
	for id, st := range stats {
		if id == shape.TrunkClassID {
			d.TrunkBytes = st.Bytes
			continue
		}
		if name, ok := shape.ClassNameForID(id); ok {
			d.LaneBytes[name] = st.Bytes
		}
	}
	return d
}

// roleOf names a lane tree's role: the shaped NIC, a pod veth, or unknown when
// the NIC could not be named, so the NIC is never taken for a pod.
func roleOf(dev string, scope verifyScope) string {
	switch {
	case scope.nicUnknown:
		return RoleUnknown
	case dev == scope.shapedNIC:
		return RoleEgress
	default:
		return RoleVeth
	}
}

// capLines truncates a diff to maxDiffLines, noting how many were dropped.
func capLines(lines []string) []string {
	if len(lines) <= maxDiffLines {
		return lines
	}
	out := append([]string(nil), lines[:maxDiffLines]...)
	return append(out, "... and "+strconv.Itoa(len(lines)-maxDiffLines)+" more")
}
