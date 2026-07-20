// SPDX-License-Identifier: Apache-2.0

package node

import (
	"encoding/json"
	"fmt"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/blocknode/shaper"
)

var (
	flagStatuszURL   string
	flagReconcileDry bool

	reconcileShaperCmd = &cobra.Command{
		Use:   "reconcile-shaper",
		Short: "Reconcile the block node traffic-shaper's nft policy membership from statusz",
		Long: "Fetch the block node's statusz inbound/outbound active endpoints, map them to the " +
			"traffic-shaper's nft policy sets, and reconcile live set membership.\n\n" +
			"With --check (unprivileged) it only fetches and prints a digest of the desired membership " +
			"and touches no nft state. Without --check (root; the daemon invokes it via sudo) it reads " +
			"the live nft sets, diffs, and rewrites only the policies whose membership changed.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagStatuszURL == "" {
				return errorx.IllegalArgument.New("--statusz-url is required")
			}

			r := shaper.NewReconciler(flagStatuszURL)

			if flagReconcileDry {
				return runReconcileCheck(cmd, r)
			}
			return runReconcileApply(cmd, r)
		},
	}
)

// runReconcileCheck runs the unprivileged detect path: fetch statusz, digest the
// desired membership, and print it.
func runReconcileCheck(cmd *cobra.Command, r *shaper.Reconciler) error {
	result, err := r.Check(cmd.Context())
	if err != nil {
		return err
	}

	if common.OutputIsJSON() {
		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return errorx.InternalError.Wrap(err, "marshal reconcile-shaper check result")
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "desired-digest: %s\n", result.Digest)
	return nil
}

// runReconcileApply runs the privileged apply path: fetch statusz, diff against
// live nft, rewrite changed policies, and print the summary.
func runReconcileApply(cmd *cobra.Command, r *shaper.Reconciler) error {
	result, err := r.Apply(cmd.Context())
	if err != nil {
		return err
	}

	if common.OutputIsJSON() {
		out, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return errorx.InternalError.Wrap(err, "marshal reconcile-shaper result")
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "applied:   %s\n", joinOrNone(result.Applied))
		fmt.Fprintf(cmd.OutOrStdout(), "skipped:   %s\n", joinOrNone(result.Skipped))
		fmt.Fprintf(cmd.OutOrStdout(), "unchanged: %s\n", joinOrNone(result.Unchanged))
		fmt.Fprintf(cmd.OutOrStdout(), "digest:    %s\n", result.Digest)
	}

	logx.As().Info().
		Strs("applied", result.Applied).
		Strs("skipped", result.Skipped).
		Strs("unchanged", result.Unchanged).
		Str("digest", result.Digest).
		Msg("block node traffic-shaper membership reconciled")
	return nil
}

// joinOrNone renders a policy-name list for text output, using "(none)" for an
// empty list so the operator sees an explicit result rather than a blank line.
func joinOrNone(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	out := names[0]
	for _, n := range names[1:] {
		out += ", " + n
	}
	return out
}

func init() {
	// This is a narrow worker subprocess (the daemon scheduler execs it, or an
	// operator runs it by hand for debugging), not a provisioning command: it
	// only talks to statusz and the already-provisioned nft sets, so it needs
	// neither the weaver-installation check nor startup migrations. Both read
	// root-owned state (see RunPersistentPreRun), which would otherwise defeat
	// --check's whole point of running unprivileged. root.go's superuser gate
	// still independently requires root for the apply path.
	common.SkipGlobalChecks(reconcileShaperCmd)

	reconcileShaperCmd.Flags().StringVar(&flagStatuszURL, "statusz-url", "",
		"Base URL of the block node's statusz endpoints (required)")
	reconcileShaperCmd.Flags().BoolVar(&flagReconcileDry, "check", false,
		"Only fetch and print the desired-membership digest; read/write no nft state (unprivileged)")
	_ = reconcileShaperCmd.MarkFlagRequired("statusz-url")
}
