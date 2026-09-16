// SPDX-License-Identifier: Apache-2.0

// Package reassert wires the `solo-provisioner network reassert` verb to the
// internal/network/reassert engine. It checks that weaver's live network state
// — the two nftables tables and the $EGRESS HTB root qdisc — is still in the
// kernel and restarts the systemd unit that replays whatever is missing.
//
// The solo-provisioner-daemon execs this once a minute via sudo; an operator
// runs it by hand to inspect state (--check) or to recover a node with no
// daemon.
package reassert

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	ra "github.com/hashgraph/solo-weaver/internal/network/reassert"
)

var (
	flagReassertCheck bool

	reassertCmd = &cobra.Command{
		Use:   "reassert",
		Short: "Restore weaver's nft tables and the $EGRESS HTB hierarchy if a third party removed them",
		Long: "Check that weaver's live network state is still in the kernel and restore whatever is " +
			"missing: the `inet weaver-host-firewall` table, the `inet weaver-workload-policy` table, " +
			"and the $EGRESS HTB root qdisc.\n\n" +
			"These are destroyed silently by things weaver does not control — stock /etc/nftables.conf " +
			"opens with `flush ruleset`, so any start of nftables.service takes both tables; `netplan " +
			"apply` or a stray `tc qdisc del` takes the shaping hierarchy. Nothing errors and the node " +
			"keeps serving traffic, just unfiltered and unshaped.\n\n" +
			"Each plane is checked only where it is provisioned, decided from its own persisted " +
			"artifact, and repair means restarting the systemd unit that replays that artifact — " +
			"nothing is re-rendered. An artifact whose presence cannot be determined is reported and " +
			"left alone rather than rebuilt on a guess.\n\n" +
			"This normally runs unattended: the solo-provisioner-daemon execs it via sudo once a " +
			"minute. Run it by hand to inspect state (--check), or to recover a node that has no " +
			"daemon.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			r := ra.New()
			if flagReassertCheck {
				return emitReassertReport(cmd, r.Check(cmd.Context()), true)
			}
			return emitReassertReport(cmd, r.Reassert(cmd.Context()), false)
		},
	}
)

// emitReassertReport renders the report as JSON or a table, then logs a summary.
func emitReassertReport(cmd *cobra.Command, report ra.Report, checkOnly bool) error {
	if common.OutputIsJSON() {
		// One compact line tagged "type":"reassert". Log events go to stderr in
		// this mode, so stdout is the report alone.
		out, err := json.Marshal(report)
		if err != nil {
			return errorx.InternalError.Wrap(err, "marshal network reassert report")
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
	} else {
		writeReassertTable(cmd, report)
	}

	logReassertOutcome(report, checkOnly)
	return nil
}

// writeReassertTable prints one row per artifact.
func writeReassertTable(cmd *cobra.Command, report ra.Report) {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ARTIFACT\tSTATE\tDETAIL")
	for _, a := range report.Artifacts {
		fmt.Fprintf(w, "%s\t%s\t%s\n", a.Artifact, reassertState(a), a.Detail)
	}
	_ = w.Flush()
}

// reassertState collapses one artifact's flags into a single word. Order
// matters: skipped and unknown outrank the rest.
func reassertState(a ra.ArtifactStatus) string {
	switch {
	case a.Skipped:
		return "skipped"
	case a.ProbeFailed:
		return "unknown"
	case !a.Expected:
		return "not-provisioned"
	case a.Reasserted && a.Recovered:
		return "restored"
	case a.Reasserted:
		return "UNRECOVERED"
	case a.Present:
		return "present"
	default:
		return "MISSING"
	}
}

// logReassertOutcome emits one journal line summarising the run. Restores log
// at WARN because something outside weaver destroyed live state.
func logReassertOutcome(report ra.Report, checkOnly bool) {
	restored := make([]string, 0, len(report.Artifacts))
	for _, a := range report.Reasserted() {
		if a.Recovered {
			restored = append(restored, a.Artifact)
		}
	}
	unhealthy := artifactNames(report.Unhealthy())
	// Only non-empty under --check; a normal run repairs what it finds absent.
	missing := artifactNames(report.Missing())
	skipped := artifactNames(report.Skipped())

	switch {
	case len(unhealthy) > 0:
		logx.As().Error().
			Strs("unhealthy", unhealthy).
			Strs("restored", restored).
			Msg("weaver network state could not be fully re-asserted")
	case len(missing) > 0:
		logx.As().Warn().
			Strs("missing", missing).
			Msg("weaver network state is missing; re-run without --check to restore it")
	case len(restored) > 0:
		logx.As().Warn().
			Strs("restored", restored).
			Msg("weaver network state was missing and has been re-asserted")
	// A run that skipped a plane must not read as one that found it healthy.
	case len(skipped) > 0:
		logx.As().Info().
			Strs("skipped", skipped).
			Msg("weaver network state was not verified; an operator apply held the lock")
	case checkOnly:
		logx.As().Info().Msg("weaver network state checked; nothing missing")
	default:
		logx.As().Debug().Msg("weaver network state verified; nothing to re-assert")
	}
}

// artifactNames projects a status slice to its artifact names.
func artifactNames(in []ra.ArtifactStatus) []string {
	out := make([]string, 0, len(in))
	for _, a := range in {
		out = append(out, a.Artifact)
	}
	return out
}

func init() {
	// The daemon execs this every minute; the global pre-run would write under
	// /usr/lib, which is read-only there. The root gate in root.go still applies.
	common.SkipGlobalChecks(reassertCmd)

	reassertCmd.Flags().BoolVar(&flagReassertCheck, "check", false,
		"Report what is missing without restoring anything")
}

// GetCmd returns the `network reassert` command.
func GetCmd() *cobra.Command {
	return reassertCmd
}
