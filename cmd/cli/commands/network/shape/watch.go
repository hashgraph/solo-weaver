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
		"Read-only: it never mutates tc or the shape registry. Both --device (egress or ingress) and " +
		"--iface (the interface to sample) are required — the command does no environment probing (no NIC " +
		"or veth auto-detection), so it stays independent of any running block node. For egress, --iface " +
		"is the physical NIC (e.g. enp0s1); for ingress, the per-pod host veth (e.g. lxc1a2b3c). --class " +
		"narrows the output to a single class within --device. Runs until interrupted (Ctrl-C) unless " +
		"--count is given.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if flagDevice == "" {
			return errorx.IllegalArgument.New("--device is required (egress or ingress)")
		}
		if flagWatchIface == "" {
			return errorx.IllegalArgument.New("--iface is required (the interface to sample)")
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
	watchCmd.Flags().StringVar(&flagClass, "class", "", "Narrow output to a single class within --device (optional)")
	watchCmd.Flags().StringVar(&flagDevice, "device", "", "Traffic direction to watch: egress ($NIC) or ingress ($VETH) (required)")
	watchCmd.Flags().StringVar(&flagWatchIface, "iface", "", "Interface to sample, e.g. enp0s1 (egress) or lxc1a2b3c (ingress veth) (required)")
	watchCmd.Flags().DurationVar(&flagWatchInterval, "interval", 2*time.Second, "Sampling interval (e.g. 1s, 500ms)")
	watchCmd.Flags().IntVar(&flagWatchCount, "count", 0, "Number of samples to print then exit (0 = run until interrupted)")
}
