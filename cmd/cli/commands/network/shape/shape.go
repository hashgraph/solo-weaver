// SPDX-License-Identifier: Apache-2.0

// Package shape wires the `solo-provisioner network shape` verbs to the
// internal/network/shape manager. The shape scope manages the tc HTB bandwidth
// plane on $EGRESS (physical NIC) and $VETH (pod traffic), maintaining
// per-class rate/ceil/prio configurations that drive the tc-egress.sh boot
// script and the daemon pod-lifecycle watcher (TS_3).
//
// Two forms of `create` are supported, mutually exclusive via --class/--device:
//   - create --device <dir> --rate <speed> --default <class>: configure the
//     root HTB qdisc and trunk class for "ingress" ($VETH) or "egress" ($NIC).
//   - create --class <name> --rate <speed> [--ceil <speed>] [--prio <n>]:
//     add an HTB leaf class; device is implied by the class direction (§5).
package shape

import (
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	shp "github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/spf13/cobra"
)

// Shared flag binding targets. Only one verb runs per invocation.
var (
	flagClass   string
	flagDevice  string
	flagRate    string
	flagCeil    string
	flagPrio    int
	flagDefault string
)

var shapeCmd = &cobra.Command{
	Use:   "shape",
	Short: "Manage tc HTB bandwidth classes on $EGRESS and $VETH",
	Long: "Manage the tc HTB bandwidth plane: configure root qdisc devices and per-class " +
		"rate/ceil/prio bandwidth parameters. Egress mutations re-render " +
		"solo-provisioner-tc-egress.sh and restart the boot service. Ingress mutations " +
		"write config for the daemon's pod-lifecycle watcher (TS_3) to apply to each new veth interface.",
	RunE: common.DefaultRunE,
}

func init() {
	shapeCmd.AddCommand(createCmd)
	shapeCmd.AddCommand(setCmd)
	shapeCmd.AddCommand(showCmd)
	shapeCmd.AddCommand(deleteCmd)
}

// GetCmd returns the root of the `network shape` command group.
func GetCmd() *cobra.Command {
	return shapeCmd
}

// newManager constructs the production manager. Indirected through a var so
// command tests can stub it.
var newManager = func() *shp.Manager { return shp.NewManager() }
