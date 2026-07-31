// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMonotonicDelta(t *testing.T) {
	require.Equal(t, uint64(0), monotonicDelta(0, 0))
	require.Equal(t, uint64(5), monotonicDelta(10, 15))
	require.Equal(t, uint64(15), monotonicDelta(0, 15))
	// Counter reset (qdisc reinstalled between samples): clamp to 0, never wrap.
	require.Equal(t, uint64(0), monotonicDelta(100, 3))
}

func TestClassDeltas(t *testing.T) {
	prev := map[string]ClassStat{
		"1:40": {ClassID: "1:40", Bytes: 1_000, Overlimits: 2, Drops: 0},
		"1:50": {ClassID: "1:50", Bytes: 500, Overlimits: 0, Drops: 1},
	}
	cur := map[string]ClassStat{
		"1:40": {ClassID: "1:40", Bytes: 126_000, Overlimits: 7, Drops: 0}, // +125000 B over 1s → 1_000_000 bit/s
		"1:50": {ClassID: "1:50", Bytes: 400, Overlimits: 3, Drops: 4},     // bytes went backwards → 0
		// reserve-egress (1:60) absent from cur → skipped
	}

	got := classDeltas(prev, cur, []string{"partner", "public", "reserve-egress"}, time.Second)
	require.Len(t, got, 2, "reserve-egress absent from the live device must be skipped")

	// Ordered by classid: partner (1:40) before public (1:50).
	require.Equal(t, "partner", got[0].Name)
	require.Equal(t, "1:40", got[0].ClassID)
	require.Equal(t, uint64(125_000), got[0].BytesDelta)
	require.InDelta(t, 1_000_000.0, got[0].RateBitsPerSec, 0.001)
	require.Equal(t, uint64(5), got[0].OverlimitsDelta)
	require.Equal(t, uint64(0), got[0].DropsDelta)

	require.Equal(t, "public", got[1].Name)
	require.Equal(t, uint64(0), got[1].BytesDelta, "backwards byte counter clamps to 0")
	require.Equal(t, uint64(3), got[1].OverlimitsDelta)
	require.Equal(t, uint64(3), got[1].DropsDelta)
}

func TestClassDeltas_FirstSampleFromZeroBaseline(t *testing.T) {
	// prev empty (a class just appeared): full current value is the delta.
	cur := map[string]ClassStat{"1:40": {ClassID: "1:40", Bytes: 2_000}}
	got := classDeltas(map[string]ClassStat{}, cur, []string{"partner"}, time.Second)
	require.Len(t, got, 1)
	require.Equal(t, uint64(2_000), got[0].BytesDelta)
}

func TestHumanRate(t *testing.T) {
	require.Equal(t, "0 bit/s", humanRate(0))
	require.Equal(t, "1.00 Kbit/s", humanRate(1_000))
	require.Equal(t, "1.00 Mbit/s", humanRate(1_000_000))
	require.Equal(t, "2.50 Gbit/s", humanRate(2_500_000_000))
}

func TestHumanBytes(t *testing.T) {
	require.Equal(t, "512 B", humanBytes(512))
	require.Equal(t, "1.00 KB", humanBytes(1024))
	require.Equal(t, "1.00 MB", humanBytes(1024*1024))
}

// scriptedStatsRunner returns a fixed sequence of samples from ClassStats,
// repeating the last one once exhausted. Write verbs are inherited (unused).
type scriptedStatsRunner struct {
	recordingTCRunner
	samples []map[string]ClassStat
	idx     int
	calls   int
}

func (r *scriptedStatsRunner) ClassStats(ctx context.Context, _ string) (map[string]ClassStat, error) {
	// Mirror the real runner: a cancelled context surfaces as an error, which
	// WatchClasses must treat as a clean stop.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	i := r.idx
	if i >= len(r.samples) {
		i = len(r.samples) - 1
	}
	r.idx++
	r.calls++
	return r.samples[i], nil
}

func TestWatchClasses_CountBoundedLoop(t *testing.T) {
	tc := &scriptedStatsRunner{samples: []map[string]ClassStat{
		{"1:40": {ClassID: "1:40", Bytes: 0}},
		{"1:40": {ClassID: "1:40", Bytes: 125_000, Overlimits: 1}},
		{"1:40": {ClassID: "1:40", Bytes: 250_000, Overlimits: 3}},
	}}
	m := NewManagerWithConfig(Config{
		NICDetect: func() (string, error) { return "eth0", nil },
		TCRunner:  tc,
	})

	var buf bytes.Buffer
	spec := WatchSpec{Class: "partner", Interval: time.Millisecond, Count: 2}
	require.NoError(t, m.WatchClasses(context.Background(), spec, &buf))

	// Baseline + 2 delta samples = 3 ClassStats calls.
	require.Equal(t, 3, tc.calls)
	out := buf.String()
	require.Contains(t, out, "watching tc classes on eth0 (egress device)")
	require.Contains(t, out, "CLASS")
	require.Contains(t, out, "partner")
	require.Contains(t, out, "1:40")
}

func TestWatchClasses_CtxCancelStops(t *testing.T) {
	tc := &scriptedStatsRunner{samples: []map[string]ClassStat{
		{"1:40": {ClassID: "1:40", Bytes: 0}},
	}}
	m := NewManagerWithConfig(Config{
		NICDetect: func() (string, error) { return "eth0", nil },
		TCRunner:  tc,
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled: the baseline sample errors on the cancelled ctx

	var buf bytes.Buffer
	// A cancelled context during the (baseline) tc sample is a clean stop, not an
	// error — WatchClasses must return nil rather than bubbling up the ctx error.
	err := m.WatchClasses(ctx, WatchSpec{Device: "egress", Interval: time.Hour}, &buf)
	require.NoError(t, err)
}

func TestWatchClasses_IngressRequiresIface(t *testing.T) {
	m := NewManagerWithConfig(Config{
		NICDetect: func() (string, error) { return "eth0", nil },
		TCRunner:  &scriptedStatsRunner{samples: []map[string]ClassStat{{}}},
	})
	err := m.WatchClasses(context.Background(), WatchSpec{Device: "ingress", Interval: time.Second}, &bytes.Buffer{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--iface")
}

func TestWatchClasses_BadInterval(t *testing.T) {
	m := NewManagerWithConfig(Config{TCRunner: &scriptedStatsRunner{samples: []map[string]ClassStat{{}}}})
	err := m.WatchClasses(context.Background(), WatchSpec{Device: "egress", Interval: 0}, &bytes.Buffer{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--interval")
}

func TestResolveWatchClasses_DefaultsToEgress(t *testing.T) {
	dir, classes, err := resolveWatchClasses(WatchSpec{})
	require.NoError(t, err)
	require.Equal(t, DirEgress, dir)
	require.ElementsMatch(t, []string{"partner", "public", "reserve-egress"}, classes)
}

func TestResolveWatchClasses_IngressDevice(t *testing.T) {
	dir, classes, err := resolveWatchClasses(WatchSpec{Device: "ingress"})
	require.NoError(t, err)
	require.Equal(t, DirIngress, dir)
	require.ElementsMatch(t, []string{"publisher", "backfill-response", "reserve-ingress"}, classes)
}

func TestResolveWatchClasses_UnknownClass(t *testing.T) {
	_, _, err := resolveWatchClasses(WatchSpec{Class: "nope"})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "unknown class"))
}
