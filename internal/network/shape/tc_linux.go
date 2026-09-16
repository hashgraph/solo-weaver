// SPDX-License-Identifier: Apache-2.0

//go:build linux

package shape

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"

	"github.com/joomcode/errorx"
)

// tcBin is the absolute path to the tc binary. Absolute (never bare "tc" off
// PATH) so a caller cannot hijack the privileged invocation via an
// attacker-controlled directory (see docs/dev/security-model.md).
const tcBin = "/sbin/tc"

// tcClassJSON is the subset of one element of `tc -s -j class show` output we
// consume. iproute2 emits the counters at the top level of each class object;
// the optional nested "stats" object is decoded too as a defensive fallback for
// versions that nest them, preferring whichever is populated.
type tcClassJSON struct {
	Handle     string `json:"handle"`
	Bytes      uint64 `json:"bytes"`
	Packets    uint64 `json:"packets"`
	Drops      uint64 `json:"drops"`
	Overlimits uint64 `json:"overlimits"`
	Stats      *struct {
		Bytes      uint64 `json:"bytes"`
		Packets    uint64 `json:"packets"`
		Drops      uint64 `json:"drops"`
		Overlimits uint64 `json:"overlimits"`
	} `json:"stats"`
}

type execTCRunner struct{}

// run execs `tc <args...>` and wraps any non-zero exit with the combined output.
func (r *execTCRunner) run(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, tcBin, args...).CombinedOutput()
	if err != nil {
		return errorx.ExternalError.Wrap(err,
			"tc %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return nil
}

func (r *execTCRunner) ClassChange(ctx context.Context, nic, minor, rate, ceil string, prio int) error {
	return r.run(ctx, tcClassChangeArgs(nic, minor, rate, ceil, prio)...)
}

func (r *execTCRunner) QdiscDelRoot(ctx context.Context, nic string) error {
	// Best-effort: swallow the error so a fresh veth (no root qdisc yet) or a
	// recycled veth name both start from a clean rebuild.
	_ = exec.CommandContext(ctx, tcBin, tcQdiscDelRootArgs(nic)...).Run()
	return nil
}

func (r *execTCRunner) QdiscAddRoot(ctx context.Context, nic, defaultMinor string) error {
	return r.run(ctx, tcQdiscAddRootArgs(nic, defaultMinor)...)
}

func (r *execTCRunner) ClassAddRoot(ctx context.Context, nic, rate, ceil string) error {
	return r.run(ctx, tcClassAddRootArgs(nic, rate, ceil)...)
}

func (r *execTCRunner) ClassAdd(ctx context.Context, nic, minor, rate, ceil string, prio int) error {
	return r.run(ctx, tcClassAddArgs(nic, minor, rate, ceil, prio)...)
}

func (r *execTCRunner) QdiscAddFqCodel(ctx context.Context, nic, minor, handle string) error {
	return r.run(ctx, tcQdiscAddFqCodelArgs(nic, minor, handle)...)
}

func (r *execTCRunner) ClassStats(ctx context.Context, dev string) (map[string]ClassStat, error) {
	cmd := exec.CommandContext(ctx, tcBin, "-s", "-j", "class", "show", "dev", dev)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err,
			"tc -s -j class show dev %s failed: %s", dev, strings.TrimSpace(stderr.String()))
	}

	var raw []tcClassJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, errorx.ExternalError.Wrap(err,
			"failed to parse tc class stats JSON for dev %s", dev)
	}

	stats := make(map[string]ClassStat, len(raw))
	for _, c := range raw {
		if c.Handle == "" {
			continue // qdisc-only or malformed entry
		}
		s := ClassStat{
			ClassID:    c.Handle,
			Bytes:      c.Bytes,
			Packets:    c.Packets,
			Drops:      c.Drops,
			Overlimits: c.Overlimits,
		}
		// Fallback only when the flat form carried nothing: some iproute2 builds
		// nest the counters under "stats" instead. Never overwrite populated
		// top-level values with a (possibly empty) nested object.
		if c.Stats != nil && s.Bytes == 0 && s.Packets == 0 && s.Drops == 0 && s.Overlimits == 0 {
			s.Bytes = c.Stats.Bytes
			s.Packets = c.Stats.Packets
			s.Drops = c.Stats.Drops
			s.Overlimits = c.Stats.Overlimits
		}
		stats[c.Handle] = s
	}
	return stats, nil
}

// tcQdiscJSON is the subset of `tc -j qdisc show` output the probe reads.
// iproute2 emits "root": true or "parent", never "root": false.
type tcQdiscJSON struct {
	Kind   string `json:"kind"`
	Handle string `json:"handle"`
	Parent string `json:"parent"`
	Root   *bool  `json:"root"`
}

// isWeaverRoot reports whether this entry is weaver's `root handle 1: htb`
// qdisc, not a foreign hierarchy's child HTB that happens to reuse handle 1:.
func (q tcQdiscJSON) isWeaverRoot() bool {
	if q.Kind != "htb" || strings.TrimSuffix(q.Handle, ":") != "1" {
		return false
	}
	if q.Parent != "" {
		return false
	}
	// A missing "root" is tolerated for iproute2 builds that omit the field.
	return q.Root == nil || *q.Root
}

// QdiscRootExists implements TCRunner.
func (r *execTCRunner) QdiscRootExists(ctx context.Context, dev string) (bool, error) {
	cmd := exec.CommandContext(ctx, tcBin, "-j", "qdisc", "show", "dev", dev)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// A missing device lands here: "cannot determine", not "no qdisc".
		return false, errorx.ExternalError.Wrap(err,
			"tc -j qdisc show dev %s failed: %s", dev, strings.TrimSpace(stderr.String()))
	}

	return qdiscRootPresent(out, dev)
}

// qdiscRootPresent decides whether `tc -j qdisc show` output declares weaver's
// root qdisc. Split from the exec so the "cannot determine" branches are
// testable without a kernel.
func qdiscRootPresent(out []byte, dev string) (bool, error) {
	// Empty stdout would decode as an empty list; reject it rather than read it
	// as "root qdisc gone".
	if len(bytes.TrimSpace(out)) == 0 {
		return false, errorx.ExternalError.New(
			"tc -j qdisc show dev %s produced no output; cannot determine whether the root qdisc is present", dev)
	}

	var raw []tcQdiscJSON
	if err := json.Unmarshal(out, &raw); err != nil {
		return false, errorx.ExternalError.Wrap(err,
			"failed to parse tc qdisc JSON for dev %s", dev)
	}

	for _, q := range raw {
		if q.isWeaverRoot() {
			return true, nil
		}
	}
	return false, nil
}

// newExecTCRunner returns the production TC runner that shells out to /sbin/tc.
func newExecTCRunner() TCRunner {
	return &execTCRunner{}
}
