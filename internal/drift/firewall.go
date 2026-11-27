// SPDX-License-Identifier: Apache-2.0

package drift

import (
	"slices"
	"strconv"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/state"
)

const firewallComponent = "firewall"

// Firewall reports where the host firewall differs from what is expected of
// it. The recorded decision is a choice the operator made, so it is compared
// and never replaced by what the host shows: a table live on a host recorded
// as disabled is reported on "disabled". Otherwise the host's firewall config
// says whether a table should be live, reported on "configFile".
//
// It compares only what the run's firewall reader found before the workflow;
// with no reading it reports nothing.
func Firewall(baseline, live state.State) []Change {
	observed := live.MachineState.FirewallObserved
	if observed == nil {
		return nil
	}
	record := baseline.MachineState.Firewall
	recordedDisabled := record != nil && record.Disabled

	// A table that could not be read is reported once, on the field a good
	// read would report on, but only where a table is expected. With no config
	// none is, so a failed read there is not reported, unlike the Producer rule.
	if observed.Unobserved {
		switch {
		case !observed.ConfigPresent:
			return nil
		case recordedDisabled:
			return []Change{{Component: firewallComponent, Field: "disabled", Persisted: strconv.FormatBool(true), Unobserved: true}}
		default:
			return []Change{{Component: firewallComponent, Field: "configFile", Persisted: presence(true), Unobserved: true}}
		}
	}

	if recordedDisabled {
		if len(observed.Fields) > 0 || len(observed.Rules) > 0 {
			logx.As().Debug().Str("component", firewallComponent).
				Msg("host firewall content not compared: recorded as disabled, so no table is expected")
		}
		if !observed.TablePresent {
			return nil
		}
		return []Change{{
			Component: firewallComponent,
			Field:     "disabled",
			Persisted: strconv.FormatBool(true),
			Live:      strconv.FormatBool(false),
		}}
	}

	if observed.ConfigPresent == observed.TablePresent {
		if observed.ConfigUnreadable {
			return []Change{{Component: firewallComponent, Field: "configFile", Persisted: presence(true), Unobserved: true}}
		}
		return capFirewallChanges(compareFirewallContent(observed))
	}
	return []Change{{
		Component: firewallComponent,
		Field:     "configFile",
		Persisted: presence(observed.ConfigPresent),
		Live:      presence(observed.TablePresent),
		Removed:   !observed.TablePresent,
	}}
}

// The caps keep a firewall's lines short and few: an allowlist can hold
// hundreds of entries, and every Change is also recorded in the action history
// on every run until it is repaired.
const (
	maxFirewallItems   = 5
	maxFirewallBytes   = 200
	maxFirewallChanges = 10
)

// compareFirewallContent reports the reserved blocks' fields that differ, then
// each allow rule by name: its presence when only one side holds it, else its
// fields that differ.
func compareFirewallContent(observed *state.HostFirewallObservation) []Change {
	var changes []Change
	for _, f := range observed.Fields {
		if !strings.HasPrefix(f.Field, "configFile.allow[") {
			changes = append(changes, compareFirewallField(f)...)
		}
	}
	rules := slices.Clone(observed.Rules)
	slices.SortFunc(rules, func(a, b state.HostFirewallRuleObservation) int { return strings.Compare(a.Name, b.Name) })
	for _, r := range rules {
		key := "configFile.allow[" + r.Name + "]"
		if r.InConfig != r.Live {
			changes = append(changes, Change{
				Component: firewallComponent,
				Field:     key,
				Persisted: presence(r.InConfig),
				Live:      presence(r.Live),
				Removed:   !r.Live,
			})
			continue
		}
		for _, f := range observed.Fields {
			if strings.HasPrefix(f.Field, key+".") {
				changes = append(changes, compareFirewallField(f)...)
			}
		}
	}
	return changes
}

// compareFirewallField reports f if its live counterpart differs. Each side
// shows only what the other lacks, plus how many entries both hold, so an
// address added by hand to a long allowlist is easy to see.
func compareFirewallField(f state.HostFirewallFieldObservation) []Change {
	if slices.Equal(f.Config, f.Live) {
		return nil
	}
	onlyConfig, onlyLive, inBoth := splitByPresence(f.Config, f.Live)
	return []Change{{
		Component: firewallComponent,
		Field:     f.Field,
		Persisted: ownItems(onlyConfig, inBoth),
		Live:      ownItems(onlyLive, inBoth),
	}}
}

// capFirewallChanges keeps the first maxFirewallChanges and names the rest in
// one more Change; the reader logs them in full at debug.
func capFirewallChanges(changes []Change) []Change {
	if len(changes) <= maxFirewallChanges {
		return changes
	}
	var rest []string
	for _, c := range changes[maxFirewallChanges:] {
		rest = append(rest, c.Field)
	}
	return append(changes[:maxFirewallChanges:maxFirewallChanges], Change{
		Component: firewallComponent,
		Field:     "configFile.other",
		Persisted: ownItems(rest, 0),
		Live:      "see the debug log",
	})
}

func splitByPresence(a, b []string) (onlyA, onlyB []string, inBoth int) {
	for _, v := range a {
		if slices.Contains(b, v) {
			inBoth++
		} else {
			onlyA = append(onlyA, v)
		}
	}
	for _, v := range b {
		if !slices.Contains(a, v) {
			onlyB = append(onlyB, v)
		}
	}
	return onlyA, onlyB, inBoth
}

// ownItems renders one side of a line: up to maxFirewallItems of items, cut
// before the joined items pass maxFirewallBytes but never to none, then how
// many more there are and how many entries both sides hold.
func ownItems(items []string, inBoth int) string {
	var parts []string
	size := 0
	for _, item := range items {
		if len(parts) == maxFirewallItems || (len(parts) > 0 && size+len(", ")+len(item) > maxFirewallBytes) {
			break
		}
		if len(parts) > 0 {
			size += len(", ")
		}
		size += len(item)
		parts = append(parts, item)
	}
	if more := len(items) - len(parts); more > 0 {
		parts = append(parts, "... and "+strconv.Itoa(more)+" more")
	}
	if inBoth > 0 {
		parts = append(parts, strconv.Itoa(inBoth)+" in both")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func presence(present bool) string {
	if present {
		return "present"
	}
	return "absent"
}
