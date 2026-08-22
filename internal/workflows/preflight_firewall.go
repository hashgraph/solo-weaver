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
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
)

// conflictingFirewallManagers are host firewall managers whose rules can conflict
// with the weaver nftables tables (#982). nftables.service is excluded on purpose.
var conflictingFirewallManagers = []string{"ufw.service", "firewalld.service"}

// firewallUnitState probes one systemd unit's enabled/running state. Injectable
// so tests don't need a systemd DBus connection.
type firewallUnitState func(ctx context.Context, unit string) (enabled bool, running bool, err error)

// ufwConfPath is the file ufw-init itself consults to decide whether to load
// any rules.
const ufwConfPath = "/etc/ufw/ufw.conf"

// ufwEnabledState reports whether ufw is configured to load its ruleset.
// Injectable so tests don't touch /etc.
type ufwEnabledState func() (enabled bool, known bool)

// combineUnitProbes queries enabled/running independently, so a failed query
// only loses its own answer; it errors only if both queries fail.
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
// ENABLED key is a known "off" — that is how ufw-init reads it too.
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

// CheckFirewallManagersStep reports firewall managers running alongside the
// weaver firewall (#982). Advisory only — it never fails the workflow.
func CheckFirewallManagersStep() automa.Builder {
	return checkFirewallManagersStep(systemdFirewallUnitState, ufwRulesetEnabled)
}

func checkFirewallManagersStep(probe firewallUnitState, ufwEnabled ufwEnabledState) automa.Builder {
	return automa.NewStepBuilder().WithId("validate-firewall-managers").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			for _, unit := range conflictingFirewallManagers {
				enabled, running, err := probe(ctx, unit)
				if err != nil {
					// Can't query systemd (e.g. a container); skip, since this check is advisory.
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
					"Conflicting firewall manager detected: %s — verify its rules permit weaver traffic, "+
						"or disable it (see the log for details)", strings.Join(conflicts, ", "))
				return
			}
			notify.As().StepCompletion(ctx, stp, rpt, "No conflicting firewall managers detected")
		})
}

// reportedConflicts reads the findings back off the report, not off state the
// builder shares between runs. Ordered by the declared list, since the metadata
// is a map.
func reportedConflicts(rpt *automa.Report) []string {
	if rpt == nil {
		return nil
	}
	var conflicts []string
	for _, unit := range conflictingFirewallManagers {
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
