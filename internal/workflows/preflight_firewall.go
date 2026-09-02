// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
)

// conflictingFirewallManagers are managers whose own rules can out-vote the
// weaver tables (#982). nftables.service destroys them instead, and is probed
// separately below.
var conflictingFirewallManagers = []string{"ufw.service", "firewalld.service"}

// nftablesService loads /etc/nftables.conf, which stock distros open with
// `flush ruleset`.
const nftablesService = "nftables.service"

// reportedUnitOrder fixes the order findings are summarized in, since the
// report metadata is a map.
var reportedUnitOrder = append(append([]string{}, conflictingFirewallManagers...), nftablesService)

// firewallUnitState probes one systemd unit's enabled/running state. Injectable
// so tests don't need a systemd DBus connection.
type firewallUnitState func(ctx context.Context, unit string) (enabled bool, running bool, err error)

// ufwConfPath is the file ufw-init consults to decide whether to load any rules.
const ufwConfPath = "/etc/ufw/ufw.conf"

// ufwEnabledState reports whether ufw is configured to load its ruleset.
// Injectable so tests don't touch /etc.
type ufwEnabledState func() (enabled bool, known bool)

// nftablesConfPath is the ruleset nftables.service loads on start.
const nftablesConfPath = "/etc/nftables.conf"

// nftFlushState reports whether that ruleset flushes everything first.
// Injectable so tests don't touch /etc.
type nftFlushState func() (flushes bool, known bool)

// combineUnitProbes queries enabled/running independently, so a failed query
// only loses its own answer; it errors only if both fail.
func combineUnitProbes(ctx context.Context, unit string,
	isEnabled, isRunning func(context.Context, string) (bool, error),
) (enabled bool, running bool, err error) {
	enabled, enabledErr := isEnabled(ctx, unit)
	running, runningErr := isRunning(ctx, unit)
	if enabledErr != nil && runningErr != nil {
		return false, false, enabledErr
	}
	return enabled && enabledErr == nil, running && runningErr == nil, nil
}

func systemdFirewallUnitState(ctx context.Context, unit string) (bool, bool, error) {
	return combineUnitProbes(ctx, unit, soos.IsServiceEnabled, soos.IsServiceRunning)
}

// ufwRulesetEnabled reads ENABLED= from /etc/ufw/ufw.conf. known is false if the
// file can't be read.
func ufwRulesetEnabled() (bool, bool) {
	return ufwRulesetEnabledAt(ufwConfPath)
}

// ufwRulesetEnabledAt is ufwRulesetEnabled with its path injected. A missing
// ENABLED key is a known "off", as ufw-init reads it too.
func ufwRulesetEnabledAt(path string) (bool, bool) {
	f, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != "ENABLED" {
			continue
		}
		return strings.EqualFold(strings.TrimSpace(value), "yes"), true
	}
	if scanner.Err() != nil {
		return false, false
	}
	return false, true
}

// nftConfFlushesRuleset reports whether /etc/nftables.conf flushes the ruleset.
// known is false if the file can't be read.
func nftConfFlushesRuleset() (bool, bool) {
	return nftConfFlushesRulesetAt(nftablesConfPath)
}

// nftConfFlushesRulesetAt is nftConfFlushesRuleset with its path injected. Only
// the top-level file is read, so a flush behind an `include` is not seen.
func nftConfFlushesRulesetAt(path string) (bool, bool) {
	f, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "flush ruleset") {
			return true, true
		}
	}
	if scanner.Err() != nil {
		return false, false
	}
	return false, true
}

// nftablesFlushRisk reports whether nftables.service can destroy the weaver
// tables: it must be enabled at boot or running, and its ruleset must flush.
// Unknown counts as risky.
func nftablesFlushRisk(ctx context.Context, probe firewallUnitState, nftFlushes nftFlushState) (string, bool) {
	enabled, running, err := probe(ctx, nftablesService)
	if err != nil {
		logx.As().Debug().Err(err).Str("unit", nftablesService).
			Msg("could not query systemd for firewall manager state; skipping check")
		return "", false
	}
	if !enabled && !running {
		return "", false
	}
	if flushes, known := nftFlushes(); known && !flushes {
		logx.As().Debug().Str("unit", nftablesService).
			Str("state", describeUnitState(enabled, running)).
			Msg("nftables.service is present but " + nftablesConfPath + " does not flush the ruleset")
		return "", false
	}
	return describeUnitState(enabled, running), true
}

// CheckFirewallManagersStep reports firewall managers running alongside the
// weaver firewall (#982). Advisory only — it never fails the workflow.
func CheckFirewallManagersStep() automa.Builder {
	return checkFirewallManagersStep(systemdFirewallUnitState, ufwRulesetEnabled, nftConfFlushesRuleset)
}

func checkFirewallManagersStep(probe firewallUnitState, ufwEnabled ufwEnabledState, nftFlushes nftFlushState) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-firewall-managers").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			for _, unit := range conflictingFirewallManagers {
				enabled, running, err := probe(ctx, unit)
				if err != nil {
					// No systemd (e.g. a container); this check is advisory, so skip.
					logx.As().Debug().Err(err).Str("unit", unit).
						Msg("could not query systemd for firewall manager state; skipping check")
					continue
				}
				if !enabled && !running {
					continue
				}
				if unit == "ufw.service" {
					// ufw.service can be active with ENABLED=no, meaning no ruleset loaded.
					if on, known := ufwEnabled(); known && !on {
						logx.As().Debug().Str("unit", unit).
							Msg("ufw.service is active but ENABLED=no in " + ufwConfPath + "; no ruleset is loaded")
						continue
					}
				}

				state := describeUnitState(enabled, running)
				meta[unit] = state
				notify.As().StepDetail(ctx, stp, "detected %s: %s", unit, state)

				logx.As().Warn().
					Str("unit", unit).
					Str("state", state).
					Msg("host firewall manager is active alongside the weaver firewall — " +
						"nftables evaluates all tables, so a drop verdict in its table overrides " +
						"weaver's accept rules. Verify its ruleset permits the traffic weaver allows " +
						"(SSH/management allowlist, cluster and block-node ports), or disable it: " +
						"sudo systemctl disable --now " + unit)
			}

			if state, risky := nftablesFlushRisk(ctx, probe, nftFlushes); risky {
				meta[nftablesService] = state
				notify.As().StepDetail(ctx, stp, "detected %s: %s", nftablesService, state)

				logx.As().Warn().
					Str("unit", nftablesService).
					Str("state", state).
					Msg("nftables.service loads a ruleset that flushes every table first, weaver's " +
						"included. Only boot replays them automatically; a restart, stop, reload or a " +
						"manual `nft flush ruleset` does not — after any of those, run: " +
						"sudo systemctl restart " + firewall.NetworkNftService +
						" (see docs/dev/traffic-shaper.md)")
			}

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking for conflicting firewall managers")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Checking for conflicting firewall managers")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			if conflicts := reportedConflicts(rpt); len(conflicts) > 0 {
				notify.As().StepCompletion(ctx, stp, rpt,
					"Firewall managers to review: %s — see the log for the risk each one carries "+
						"and what to do about it", strings.Join(conflicts, ", "))
				return
			}
			notify.As().StepCompletion(ctx, stp, rpt, "No conflicting firewall managers detected")
		})
}

// reportedConflicts reads the findings back off the report, not off state the
// builder shares between runs, ordered by reportedUnitOrder.
func reportedConflicts(rpt *automa.Report) []string {
	if rpt == nil {
		return nil
	}
	var conflicts []string
	for _, unit := range reportedUnitOrder {
		if state, ok := rpt.Metadata[unit]; ok {
			conflicts = append(conflicts, fmt.Sprintf("%s (%s)", unit, state))
		}
	}
	return conflicts
}

func describeUnitState(enabled, running bool) string {
	switch {
	case enabled && running:
		return "enabled, running"
	case enabled:
		return "enabled, not running"
	default:
		return "running, not enabled"
	}
}
