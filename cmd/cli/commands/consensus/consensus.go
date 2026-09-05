// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"fmt"
	"os"
	"strconv"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/migration"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/network"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/consensus/node"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// consensusEnabledEnv opts into the experimental consensus commands. Nothing in
// the consensus stack is production-ready yet, so the whole group is hidden from
// --help and refuses to run unless the operator explicitly opts in (this env var
// set to a truthy value, or the hidden --experimental flag). This keeps a
// block-node operator — who runs this same binary in production — from
// discovering and half-running consensus install against a production cluster.
const consensusEnabledEnv = "SOLO_ENABLE_CONSENSUS"

var flagExperimental bool

var consensusCmd = &cobra.Command{
	Use:    "consensus",
	Short:  "Manage consensus-node lifecycle and migration",
	Long:   "Commands for managing consensus-node operations including migration soak lifecycle.",
	Hidden: true,
	// Cobra runs only the nearest PersistentPreRunE in the chain, so this must
	// invoke the root's pre-run (global checks + startup migrations) itself before
	// enforcing the experimental gate. `--help` is handled before PreRun runs, so
	// the commands stay self-documenting even while gated.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if err := common.RunPersistentPreRun(cmd, args); err != nil {
			return err
		}
		return requireConsensusEnabled()
	},
	RunE: common.DefaultRunE,
}

func init() {
	consensusCmd.PersistentFlags().BoolVar(&flagExperimental, "experimental", false,
		"Enable the experimental consensus commands (not supported in production)")
	_ = consensusCmd.PersistentFlags().MarkHidden("experimental")

	consensusCmd.AddCommand(migration.GetCmd())
	consensusCmd.AddCommand(node.GetCmd())
	consensusCmd.AddCommand(network.GetCmd())
}

// requireConsensusEnabled gates execution of every consensus subcommand behind an
// explicit opt-in. It returns a decorated error unless the --experimental flag is
// set or SOLO_ENABLE_CONSENSUS holds a truthy value. Under sudo the flag is the
// reliable opt-in, since sudo drops the environment by default.
func requireConsensusEnabled() error {
	if flagExperimental {
		return nil
	}
	if v, ok := os.LookupEnv(consensusEnabledEnv); ok {
		if enabled, err := strconv.ParseBool(v); err == nil && enabled {
			return nil
		}
	}
	return errx.Decorate(
		errorx.RejectedOperation.New("consensus commands are experimental and not supported in production"),
		reasons.PreconditionNotMet,
		"These commands are under active development and not production-ready.",
		fmt.Sprintf("To use them in a non-production environment, pass --experimental (reliable under sudo) or set %s=1.", consensusEnabledEnv))
}

// GetCmd returns the consensus command group.
func GetCmd() *cobra.Command {
	return consensusCmd
}
