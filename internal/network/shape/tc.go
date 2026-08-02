// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"context"
	"strconv"
)

// TCRunner abstracts live kernel tc qdisc/class operations for testability. The
// egress path drives ClassChange (live tuning of an already-installed
// hierarchy); the ingress path drives the qdisc/class add verbs to install the
// per-veth HTB hierarchy from scratch on each BN pod create.
//
// The interface lives in this build-tag-free file so both the Linux
// (execTCRunner) and non-Linux (noopTCRunner) implementations, and the
// platform-agnostic Manager that consumes it, share a single declaration.
type TCRunner interface {
	// ClassChange runs `tc class change dev <nic> parent 1:1 classid 1:<minor>
	// htb rate <rate> ceil <ceil> prio <prio>` on the live kernel.
	ClassChange(ctx context.Context, nic, minor, rate, ceil string, prio int) error

	// QdiscDelRoot runs `tc qdisc del dev <nic> root`, tearing down any existing
	// hierarchy (cascading to all classes and leaf qdiscs). It is best-effort: a
	// missing root qdisc (a fresh veth) is not an error, so the caller can
	// unconditionally rebuild — matching the tc-egress boot script's `|| true`.
	QdiscDelRoot(ctx context.Context, nic string) error

	// QdiscAddRoot runs `tc qdisc add dev <nic> root handle 1: htb default
	// <defaultMinor>`, installing the root HTB qdisc whose unmatched traffic
	// falls to class 1:<defaultMinor>.
	QdiscAddRoot(ctx context.Context, nic, defaultMinor string) error

	// ClassAddRoot runs `tc class add dev <nic> parent 1: classid 1:1 htb rate
	// <rate> ceil <ceil>`, the trunk class every per-class leaf attaches to.
	ClassAddRoot(ctx context.Context, nic, rate, ceil string) error

	// ClassAdd runs `tc class add dev <nic> parent 1:1 classid 1:<minor> htb
	// rate <rate> ceil <ceil> prio <prio>`, a per-class leaf under the trunk.
	ClassAdd(ctx context.Context, nic, minor, rate, ceil string, prio int) error

	// QdiscAddFqCodel runs `tc qdisc add dev <nic> parent 1:<minor> handle
	// <handle>: fq_codel`, the leaf qdisc for a class.
	QdiscAddFqCodel(ctx context.Context, nic, minor, handle string) error

	// ClassStats runs `tc -s -j class show dev <dev>` and returns each class's
	// cumulative counters keyed by tc handle (e.g. "1:40"). It is the read
	// counterpart to the write verbs above, backing `network shape watch`.
	ClassStats(ctx context.Context, dev string) (map[string]ClassStat, error)
}

// The tc*Args helpers are the single source of the tc command argument
// encoding: HTB argument order and the field set for each qdisc/class verb.
// The live path (execTCRunner, tc_linux.go) executes these arg slices, and the
// tc-egress boot-script template renders the same commands as shell text; a
// lockstep test (tc_encoding_test.go) asserts the two encodings match token for
// token so a change to one side cannot silently drift from the other. They are
// build-tag-free (this file) so the live path, the render path, and that test
// all reference one definition. nic is a literal interface name on the live
// path; the boot script passes the shell variable "$NIC".

// tcQdiscDelRootArgs builds `qdisc del dev <nic> root`.
func tcQdiscDelRootArgs(nic string) []string {
	return []string{"qdisc", "del", "dev", nic, "root"}
}

// tcQdiscAddRootArgs builds `qdisc add dev <nic> root handle 1: htb default
// <defaultMinor>`.
func tcQdiscAddRootArgs(nic, defaultMinor string) []string {
	return []string{"qdisc", "add", "dev", nic, "root", "handle", "1:", "htb", "default", defaultMinor}
}

// tcClassAddRootArgs builds `class add dev <nic> parent 1: classid 1:1 htb rate
// <rate> ceil <ceil>` — the trunk class.
func tcClassAddRootArgs(nic, rate, ceil string) []string {
	return []string{"class", "add", "dev", nic, "parent", "1:", "classid", "1:1", "htb", "rate", rate, "ceil", ceil}
}

// tcClassAddArgs builds `class add dev <nic> parent 1:1 classid 1:<minor> htb
// rate <rate> ceil <ceil> prio <prio>` — a per-class leaf under the trunk.
func tcClassAddArgs(nic, minor, rate, ceil string, prio int) []string {
	return []string{"class", "add", "dev", nic, "parent", "1:1", "classid", "1:" + minor, "htb", "rate", rate, "ceil", ceil, "prio", strconv.Itoa(prio)}
}

// tcClassChangeArgs builds `class change dev <nic> parent 1:1 classid 1:<minor>
// htb rate <rate> ceil <ceil> prio <prio>` — live tuning of an existing leaf.
func tcClassChangeArgs(nic, minor, rate, ceil string, prio int) []string {
	return []string{"class", "change", "dev", nic, "parent", "1:1", "classid", "1:" + minor, "htb", "rate", rate, "ceil", ceil, "prio", strconv.Itoa(prio)}
}

// tcQdiscAddFqCodelArgs builds `qdisc add dev <nic> parent 1:<minor> handle
// <handle>: fq_codel` — the leaf qdisc for a class.
func tcQdiscAddFqCodelArgs(nic, minor, handle string) []string {
	return []string{"qdisc", "add", "dev", nic, "parent", "1:" + minor, "handle", handle + ":", "fq_codel"}
}
