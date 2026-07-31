// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"os/signal"
	"syscall"
	"time"

	shp "github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// watch-only flag binding targets (interval/count/iface are unique to `watch`;
// --class/--device are the shared vars declared in shape.go).
var (
	flagWatchIface    string
	flagWatchInterval time.Duration
	flagWatchCount    int
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Sample live tc class counters over time (read-only)",
	Long: "Sample the live tc HTB class counters (`tc -s class show`) at a fixed interval and print " +
		"per-class rate-over-time — throughput, plus the change in overlimits and drops — each tick. " +
		"Use it to confirm traffic is actually being classified and shaped (e.g. partner traffic landing " +
		"in 1:40 with a non-zero rate and climbing overlimits).\n\n" +
		"Read-only: it never mutates tc or the shape registry. Without flags it watches the egress NIC " +
		"(auto-detected from the default route). --device ingress auto-detects the shaped per-pod $VETH " +
		"(the interface carrying an HTB qdisc); pass --iface only to override or to pick one when several " +
		"ingress veths are shaped. --class narrows the output to a single class. Runs until interrupted " +
		"(Ctrl-C) unless --count is given.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if flagClass != "" && flagDevice != "" {
			return errorx.IllegalArgument.New("--class and --device are mutually exclusive")
		}
		if flagWatchInterval <= 0 {
			return errorx.IllegalArgument.New("--interval must be positive")
		}
		if flagWatchCount < 0 {
			return errorx.IllegalArgument.New("--count must be zero or positive (0 = run until interrupted)")
		}

		// Ctrl-C (and SIGTERM) cancels the sampling loop cleanly rather than
		// aborting mid-write; WatchClasses returns nil on ctx cancellation.
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		spec := shp.WatchSpec{
			Device:   flagDevice,
			Iface:    flagWatchIface,
			Class:    flagClass,
			Interval: flagWatchInterval,
			Count:    flagWatchCount,
		}
		return newManager().WatchClasses(ctx, spec, cmd.OutOrStdout())
	},
}

func init() {
	watchCmd.Flags().StringVar(&flagClass, "class", "", "Watch a single class (device is derived from the class name)")
	watchCmd.Flags().StringVar(&flagDevice, "device", "", "Traffic direction to watch: egress ($NIC, default) or ingress ($VETH)")
	watchCmd.Flags().StringVar(&flagWatchIface, "iface", "", "Interface to sample; optional — egress auto-detects the NIC, ingress auto-detects the shaped $VETH. Pass to override, or to pick one when several ingress veths are shaped")
	watchCmd.Flags().DurationVar(&flagWatchInterval, "interval", 2*time.Second, "Sampling interval (e.g. 1s, 500ms)")
	watchCmd.Flags().IntVar(&flagWatchCount, "count", 0, "Number of samples to print then exit (0 = run until interrupted)")
}
