// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/joomcode/errorx"
)

// ClassStat is a point-in-time snapshot of one tc HTB class's cumulative
// counters, as read from `tc -s class show dev <device>`. The byte/packet/drop/
// overlimit counters are monotonic since the class (qdisc) was installed, so a
// throughput reading is the delta between two snapshots over the elapsed time.
type ClassStat struct {
	ClassID    string // tc handle, e.g. "1:40"
	Bytes      uint64
	Packets    uint64
	Drops      uint64
	Overlimits uint64
}

// ClassDelta is the change in one class's counters between two samples, plus the
// throughput implied by the byte delta over the sampling interval. It is what
// `network shape watch` prints per tick: rate-over-time, not the cumulative
// totals (which `network shape show` does not expose and dashboards cover via
// the Prometheus counters).
type ClassDelta struct {
	Name            string  // class name ("partner") when the classid is known, else the raw classid
	ClassID         string  // tc handle, e.g. "1:40"
	RateBitsPerSec  float64 // byte delta * 8 / interval seconds
	BytesDelta      uint64
	OverlimitsDelta uint64
	DropsDelta      uint64
}

// WatchSpec parameterises Manager.WatchClasses. Both Device (the traffic
// direction, which selects the class set) and Iface (the interface to sample)
// are required and operator-supplied: `network shape watch` performs no
// environment probing — no NIC or veth auto-detection — so it never depends on a
// block node running or on Kubernetes. Class optionally narrows the watch to one
// class, which must belong to Device's direction.
type WatchSpec struct {
	Device   string        // "egress" or "ingress" — required; selects the class set
	Iface    string        // interface to sample — required
	Class    string        // optional: narrow to one class in Device's direction
	Interval time.Duration // sampling interval
	Count    int           // number of delta samples to print then stop; 0 = until ctx is cancelled
}

// WatchClasses samples the live tc class counters on the resolved interface every
// spec.Interval and writes a per-class delta row (rate, bytes sent, Δoverlimits,
// Δdrops) each tick to w. It is read-only: no locks, no mutations. It returns
// when ctx is cancelled (e.g. the operator hits Ctrl-C) or, when spec.Count > 0,
// after that many delta rows have been printed.
func (m *Manager) WatchClasses(ctx context.Context, spec WatchSpec, w io.Writer) error {
	if spec.Interval <= 0 {
		return errorx.IllegalArgument.New("--interval must be positive")
	}
	if spec.Count < 0 {
		return errorx.IllegalArgument.New("--count must be zero or positive (0 = run until interrupted)")
	}
	if spec.Iface == "" {
		return errorx.IllegalArgument.New("--iface is required (the interface to sample)")
	}

	dir, classes, err := resolveWatchClasses(spec)
	if err != nil {
		return err
	}
	iface := spec.Iface

	fmt.Fprintf(w, "watching tc classes on %s (%s device), interval %s — Ctrl-C to stop\n",
		iface, dir, spec.Interval)
	writeWatchHeader(w)

	// Establish the baseline sample; deltas are printed from the second sample on.
	prev, prevAt, stopped, err := m.sampleStats(ctx, iface)
	if stopped {
		return nil
	}
	if err != nil {
		return err
	}

	ticker := time.NewTicker(spec.Interval)
	defer ticker.Stop()

	printed := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		cur, curAt, stopped, err := m.sampleStats(ctx, iface)
		if stopped {
			return nil
		}
		if err != nil {
			return err
		}
		// Rate over the ACTUAL elapsed time between the two reads (the interval
		// plus the time spent running tc), not the nominal interval — dividing by
		// spec.Interval over-reports throughput, most visibly on the first tick.
		writeWatchRows(w, classDeltas(prev, cur, classes, curAt.Sub(prevAt)))
		prev, prevAt = cur, curAt

		printed++
		if spec.Count > 0 && printed >= spec.Count {
			return nil
		}
	}
}

// sampleStats reads the live class counters and stamps the read time. A failure
// caused by context cancellation (the operator hit Ctrl-C mid-sample) is reported
// as a clean stop (stopped=true, err=nil), so WatchClasses honors its "returns nil
// on cancellation" contract even when the cancellation lands during a tc exec
// rather than while waiting on the ticker.
func (m *Manager) sampleStats(ctx context.Context, iface string) (stats map[string]ClassStat, at time.Time, stopped bool, err error) {
	stats, err = m.tcRunner.ClassStats(ctx, iface)
	if err != nil {
		if ctx.Err() != nil {
			return nil, time.Time{}, true, nil
		}
		return nil, time.Time{}, false, err
	}
	return stats, time.Now(), false, nil
}

// resolveWatchClasses validates the spec's device/class selection and returns the
// direction plus the class names to display. --device (required) selects the
// class set; --class optionally narrows it to a single class, which must belong
// to that direction. There is no defaulting or auto-detection: the operator
// states intent explicitly, keeping the command independent of the running block
// node.
func resolveWatchClasses(spec WatchSpec) (dir string, classes []string, err error) {
	if spec.Device == "" {
		return "", nil, errorx.IllegalArgument.New("--device is required (egress or ingress)")
	}
	if err := validateDir(spec.Device); err != nil {
		return "", nil, err
	}
	if spec.Class != "" {
		ci, err := lookupClassInfo(spec.Class)
		if err != nil {
			return "", nil, err
		}
		if ci.Dir != spec.Device {
			return "", nil, errorx.IllegalArgument.New(
				"--class %q is a %s class but --device %s was given", spec.Class, ci.Dir, spec.Device)
		}
		return spec.Device, []string{spec.Class}, nil
	}
	return spec.Device, knownClassNamesForDir(spec.Device), nil
}

// classDeltas computes the per-class delta between two samples for the named
// classes, in classid order. A class absent from the current sample (not
// installed on the live device) is skipped; a counter that went backwards (the
// qdisc was reinstalled between samples) yields a zero delta for that tick rather
// than a spurious huge number.
func classDeltas(prev, cur map[string]ClassStat, classes []string, elapsed time.Duration) []ClassDelta {
	secs := elapsed.Seconds()
	out := make([]ClassDelta, 0, len(classes))
	for _, name := range classes {
		ci, err := lookupClassInfo(name)
		if err != nil {
			continue
		}
		classid := "1:" + ci.Minor
		c, ok := cur[classid]
		if !ok {
			continue
		}
		p := prev[classid] // zero value when the class is new this tick
		bytesDelta := monotonicDelta(p.Bytes, c.Bytes)
		var rate float64
		if secs > 0 {
			rate = float64(bytesDelta) * 8 / secs
		}
		out = append(out, ClassDelta{
			Name:            name,
			ClassID:         classid,
			RateBitsPerSec:  rate,
			BytesDelta:      bytesDelta,
			OverlimitsDelta: monotonicDelta(p.Overlimits, c.Overlimits),
			DropsDelta:      monotonicDelta(p.Drops, c.Drops),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClassID < out[j].ClassID })
	return out
}

// monotonicDelta returns cur-prev for a monotonic counter, clamping to 0 when the
// counter appears to have reset (cur < prev) so a qdisc reinstall between samples
// does not report a bogus multi-exabyte delta.
func monotonicDelta(prev, cur uint64) uint64 {
	if cur < prev {
		return 0
	}
	return cur - prev
}

func writeWatchHeader(w io.Writer) {
	fmt.Fprintf(w, "%-18s %-8s %15s %12s %11s %8s\n",
		"CLASS", "CLASSID", "RATE", "SENT", "OVERLIMITS", "DROPPED")
}

func writeWatchRows(w io.Writer, deltas []ClassDelta) {
	for _, d := range deltas {
		fmt.Fprintf(w, "%-18s %-8s %15s %12s %11s %8s\n",
			d.Name, d.ClassID, humanRate(d.RateBitsPerSec), humanBytes(d.BytesDelta),
			"+"+strconv.FormatUint(d.OverlimitsDelta, 10),
			"+"+strconv.FormatUint(d.DropsDelta, 10))
	}
	fmt.Fprintln(w) // blank line separates ticks
}

// humanRate renders a bit/s throughput with a decimal (SI) magnitude suffix,
// matching how tc bandwidth values are expressed (Mbit/Gbit, powers of 1000).
func humanRate(bitsPerSec float64) string {
	const k = 1000.0
	switch {
	case bitsPerSec >= k*k*k:
		return fmt.Sprintf("%.2f Gbit/s", bitsPerSec/(k*k*k))
	case bitsPerSec >= k*k:
		return fmt.Sprintf("%.2f Mbit/s", bitsPerSec/(k*k))
	case bitsPerSec >= k:
		return fmt.Sprintf("%.2f Kbit/s", bitsPerSec/k)
	default:
		return fmt.Sprintf("%.0f bit/s", bitsPerSec)
	}
}

// humanBytes renders a byte count with a binary (IEC) magnitude suffix.
func humanBytes(b uint64) string {
	const k = 1024.0
	f := float64(b)
	switch {
	case f >= k*k*k:
		return fmt.Sprintf("%.2f GB", f/(k*k*k))
	case f >= k*k:
		return fmt.Sprintf("%.2f MB", f/(k*k))
	case f >= k:
		return fmt.Sprintf("%.2f KB", f/k)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
