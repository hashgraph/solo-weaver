// SPDX-License-Identifier: Apache-2.0

//go:build integration && linux

package reassert

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
)

// These destroy live state the way a third party would, then assert one Reassert
// brings it back. They need root and a provisioned host, so they run only in
// the VM (`task vm:test:integration`).
//
// Destruction is scoped to weaver's own tables by default; the destructive
// `nft flush ruleset` variant, which also takes the KUBE-* and CILIUM_* tables,
// is opt-in — see Test_Reassert_RestoresBothTablesAfterFullRulesetFlush_Integration.

// rulesetFlushEnvVar opts in to the destructive full-ruleset variant.
const rulesetFlushEnvVar = "WEAVER_IT_ALLOW_RULESET_FLUSH"

// requireRoot skips unless the test can actually mutate nft/tc.
func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("requires root to read nft tables and mutate tc")
	}
}

// requireArtifact skips when the plane under test is not provisioned here: with
// no persisted artifact there is nothing for a restart to replay.
func requireArtifact(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not provisioned on this host; run `block node install` first", path)
	}
}

// requireAnyArtifact skips when this host has no weaver network state at all.
// A test that asserts "nothing needed repair" would otherwise pass for the
// trivial reason that there is nothing here to repair.
func requireAnyArtifact(t *testing.T) {
	t.Helper()
	for _, path := range []string{firewall.HostNftPath, policy.WeaverNftPath, shape.TcEgressScriptPath} {
		if _, err := os.Stat(path); err == nil {
			return
		}
	}
	t.Skip("no weaver network artifact on this host; run `block node install` first")
}

// restoreNftOnExit re-runs the loader after the test whatever happened, so a
// failed assertion never leaves the host without its weaver tables.
func restoreNftOnExit(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if err := policy.RestartNetworkNftService(context.Background()); err != nil {
			t.Logf("WARNING: could not restore the weaver nft tables: %v", err)
		}
	})
}

// restoreShaperOnExit is the tc counterpart. Register it BEFORE removing the
// qdisc, so an early failure still restores the hierarchy.
func restoreShaperOnExit(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if err := shape.RestartTcEgressService(context.Background()); err != nil {
			t.Logf("WARNING: could not restore the $EGRESS HTB hierarchy: %v", err)
		}
	})
}

// nftTableExists shells out rather than reusing the package's runner, so the
// assertion is independent of the code under test.
func nftTableExists(t *testing.T, family, table string) bool {
	t.Helper()
	return exec.Command("/usr/sbin/nft", "list", "table", family, table).Run() == nil
}

// deleteWeaverTables removes whichever weaver tables are live, reproducing what
// a ruleset flush does to weaver without touching anyone else's tables.
//
// A plane that is not provisioned here has no table to remove, and its absence
// is already the state the caller wants. The host firewall is optional — a node
// installed without --firewall-enabled runs the policy plane alone — so
// requiring both deletions would fail every caller that gates on one plane.
// A table that is live must still delete cleanly.
func deleteWeaverTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{"weaver-host-firewall", "weaver-workload-policy"} {
		if !nftTableExists(t, "inet", table) {
			continue
		}
		require.NoError(t, exec.Command("/usr/sbin/nft", "delete", "table", "inet", table).Run(),
			"could not delete inet %s", table)
	}
}

// qdiscRootHTB reports whether dev carries an htb root qdisc, read straight from
// tc's text output for the same independence reason.
func qdiscRootHTB(t *testing.T, dev string) bool {
	t.Helper()
	out, err := exec.Command("/sbin/tc", "qdisc", "show", "dev", dev).Output()
	require.NoError(t, err, "tc qdisc show dev %s", dev)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "qdisc htb 1:") && strings.Contains(line, "root") {
			return true
		}
	}
	return false
}

// statusOf pulls one artifact out of a report.
func statusOf(t *testing.T, r Report, id string) ArtifactStatus {
	t.Helper()
	for _, a := range r.Artifacts {
		if a.Artifact == id {
			return a
		}
	}
	t.Fatalf("artifact %q missing from report %+v", id, r.Artifacts)
	return ArtifactStatus{}
}

// Test_Reassert_RestoresBothTablesAfterDeletion scopes the damage to weaver's
// own tables: both destroyed, both back after one pass.
func Test_Reassert_RestoresBothTablesAfterDeletion_Integration(t *testing.T) {
	requireRoot(t)
	requireArtifact(t, firewall.HostNftPath)
	requireArtifact(t, policy.WeaverNftPath)
	restoreNftOnExit(t)

	require.True(t, nftTableExists(t, "inet", "weaver-host-firewall"),
		"precondition: host firewall table must be live before the test removes it")
	require.True(t, nftTableExists(t, "inet", "weaver-workload-policy"),
		"precondition: workload policy table must be live before the test removes it")

	deleteWeaverTables(t)
	require.False(t, nftTableExists(t, "inet", "weaver-host-firewall"))
	require.False(t, nftTableExists(t, "inet", "weaver-workload-policy"))

	report := New().Reassert(context.Background())

	assert.True(t, nftTableExists(t, "inet", "weaver-host-firewall"),
		"host firewall table must be restored; report=%+v", report.Artifacts)
	assert.True(t, nftTableExists(t, "inet", "weaver-workload-policy"),
		"workload policy table must be restored; report=%+v", report.Artifacts)

	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy} {
		got := statusOf(t, report, id)
		assert.True(t, got.Reasserted, "%s should be reported re-asserted (%s)", id, got.Detail)
		assert.True(t, got.Recovered, "%s should be reported recovered (%s)", id, got.Detail)
		assert.True(t, got.Present, "%s is live again, so it must not report present=false", id)
	}
}

// Test_Reassert_RestoresBothTablesAfterFullRulesetFlush runs the real thing —
// `nft flush ruleset` on a provisioned node.
//
// Opt-in: the flush also takes the KUBE-* and CILIUM_* tables, breaking pod
// networking until Cilium and kube-proxy rewrite their rules. Throwaway VMs only:
//
//	WEAVER_IT_ALLOW_RULESET_FLUSH=1 task vm:test:integration \
//	  TEST_NAME='^Test_Reassert_RestoresBothTablesAfterFullRulesetFlush_Integration$'
func Test_Reassert_RestoresBothTablesAfterFullRulesetFlush_Integration(t *testing.T) {
	requireRoot(t)
	if os.Getenv(rulesetFlushEnvVar) == "" {
		t.Skipf("destructive to non-weaver nft tables; set %s=1 to run", rulesetFlushEnvVar)
	}
	requireArtifact(t, firewall.HostNftPath)
	requireArtifact(t, policy.WeaverNftPath)
	restoreNftOnExit(t)

	require.NoError(t, exec.Command("/usr/sbin/nft", "flush", "ruleset").Run())
	require.False(t, nftTableExists(t, "inet", "weaver-host-firewall"))
	require.False(t, nftTableExists(t, "inet", "weaver-workload-policy"))

	report := New().Reassert(context.Background())

	assert.True(t, nftTableExists(t, "inet", "weaver-host-firewall"),
		"host firewall table must survive a full ruleset flush; report=%+v", report.Artifacts)
	assert.True(t, nftTableExists(t, "inet", "weaver-workload-policy"),
		"workload policy table must survive a full ruleset flush; report=%+v", report.Artifacts)
}

// Test_Reassert_RehydratesSetMembership pins that the replay brings the
// daemon-owned sets back populated, not just the table structure.
func Test_Reassert_RehydratesSetMembership_Integration(t *testing.T) {
	requireRoot(t)
	requireArtifact(t, policy.WeaverNftPath)
	restoreNftOnExit(t)

	// Only meaningful once the host has membership persisted; a freshly installed
	// node that never polled statusz has nothing to rehydrate.
	doc, err := os.ReadFile(policy.WeaverNftPath)
	require.NoError(t, err)
	want := setElements(t, string(doc))
	if len(want) == 0 {
		t.Skip("no set membership persisted on this host yet")
	}

	deleteWeaverTables(t)
	report := New().Reassert(context.Background())
	require.True(t, nftTableExists(t, "inet", "weaver-workload-policy"),
		"table must be restored before membership can be checked; report=%+v", report.Artifacts)

	// Which sets are populated depends on what the block node reported, so the
	// artifact is the oracle: every member it persisted must be live again.
	out, err := exec.Command("/usr/sbin/nft", "list", "table", "inet", "weaver-workload-policy").Output()
	require.NoError(t, err)
	live := setElements(t, string(out))
	for set, members := range want {
		require.Contains(t, live, set, "set %s must be restored; live table:\n%s", set, out)
		assert.Subset(t, live[set], members,
			"set %s must come back with the members the artifact persisted", set)
	}
}

// setStartRe matches the head of an nft set declaration, `set <name> {`. The
// brace keeps `meta priority set 1:40` from matching.
var setStartRe = regexp.MustCompile(`\bset\s+([A-Za-z0-9_.-]+)\s*\{`)

// setElements parses every `set <name> { … elements = { a, b } … }` out of an
// nft document, in either the one-line artifact form or nft's multi-line
// listing. Sets without an elements clause are omitted.
func setElements(t *testing.T, doc string) map[string][]string {
	t.Helper()
	const marker = "elements = {"
	out := map[string][]string{}
	for _, m := range setStartRe.FindAllStringSubmatchIndex(doc, -1) {
		name := doc[m[2]:m[3]]
		body := braceBody(doc[m[1]-1:])
		i := strings.Index(body, marker)
		if i < 0 {
			continue
		}
		rest := body[i+len(marker):]
		j := strings.Index(rest, "}")
		require.GreaterOrEqual(t, j, 0, "unterminated elements clause in set %s", name)
		for e := range strings.SplitSeq(rest[:j], ",") {
			if e = strings.TrimSpace(e); e != "" {
				out[name] = append(out[name], e)
			}
		}
	}
	return out
}

// braceBody returns the text inside the brace that opens s, up to its match.
func braceBody(s string) string {
	depth := 0
	for i, c := range s {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[1:i]
			}
		}
	}
	return s[1:]
}

// Test_Reassert_RestoresEgressHierarchyAfterQdiscDel runs
// `tc qdisc del dev $EGRESS root`: hierarchy back after one pass.
func Test_Reassert_RestoresEgressHierarchyAfterQdiscDel_Integration(t *testing.T) {
	requireRoot(t)
	requireArtifact(t, shape.TcEgressScriptPath)

	nic, ok, err := shape.EgressNICFromScript()
	require.NoError(t, err)
	require.True(t, ok, "boot script must declare a NIC")
	require.True(t, qdiscRootHTB(t, nic),
		"precondition: the HTB hierarchy must be live on %s before the test removes it", nic)

	// Registered before the destructive call, so a failure below still restores it.
	restoreShaperOnExit(t)
	require.NoError(t, exec.Command("/sbin/tc", "qdisc", "del", "dev", nic, "root").Run())
	require.False(t, qdiscRootHTB(t, nic))

	report := New().Reassert(context.Background())

	assert.True(t, qdiscRootHTB(t, nic),
		"the $EGRESS HTB hierarchy must be restored; report=%+v", report.Artifacts)

	got := statusOf(t, report, ArtifactEgressQdisc)
	assert.True(t, got.Reasserted, "detail=%s", got.Detail)
	assert.True(t, got.Recovered, "detail=%s", got.Detail)
	assert.True(t, got.Present, "the hierarchy is live again, so present must be true")
}

// Test_Reassert_HealthyHostIsANoOp guards the steady state: on a converged host
// nothing is restarted, so the check is safe to run once a minute forever.
func Test_Reassert_HealthyHostIsANoOp_Integration(t *testing.T) {
	requireRoot(t)
	// Without an artifact every assertion below holds for the trivial reason that
	// there was nothing to check, which would make this a test of nothing.
	requireAnyArtifact(t)

	report := New().Reassert(context.Background())

	assert.Empty(t, report.Reasserted(), "a healthy host must need no repair")
	assert.Empty(t, report.Unhealthy(), "a healthy host must report nothing unhealthy")
	assert.Empty(t, report.Missing(), "a healthy host must have nothing missing")
}

// Test_Check_NeverMutates pins --check's contract against the live kernel.
func Test_Check_NeverMutates_Integration(t *testing.T) {
	requireRoot(t)
	requireArtifact(t, shape.TcEgressScriptPath)

	nic, ok, err := shape.EgressNICFromScript()
	require.NoError(t, err)
	require.True(t, ok)

	// Registered before the destructive call: an early failure must not leave the
	// host unshaped.
	restoreShaperOnExit(t)
	require.NoError(t, exec.Command("/sbin/tc", "qdisc", "del", "dev", nic, "root").Run())

	report := New().Check(context.Background())

	assert.False(t, qdiscRootHTB(t, nic), "--check must not restore anything")
	got := statusOf(t, report, ArtifactEgressQdisc)
	assert.True(t, got.Expected)
	assert.False(t, got.Present)
	assert.False(t, got.Reasserted)
	assert.Len(t, report.Missing(), 1, "the removed hierarchy must be reported missing")
}

// Test_QdiscProbe_MissingDeviceIsProbeFailure: tc erroring on a device that is
// not there must surface as an error, never as (false, nil).
func Test_QdiscProbe_MissingDeviceIsProbeFailure_Integration(t *testing.T) {
	requireRoot(t)

	present, err := shape.QdiscRootExists(context.Background(), "weaver-no-such-dev")

	require.Error(t, err, "a nonexistent device must be a probe failure, not an absent qdisc")
	assert.False(t, present)
}

// Test_QdiscProbe_UnshapedDeviceIsAbsentNotAnError is the other half: a device
// with no HTB root must report a clean absence, or repair would never trigger.
func Test_QdiscProbe_UnshapedDeviceIsAbsentNotAnError_Integration(t *testing.T) {
	requireRoot(t)

	// The loopback device always exists and is never shaped by weaver.
	present, err := shape.QdiscRootExists(context.Background(), "lo")

	require.NoError(t, err, "an existing unshaped device must be a clean absence")
	assert.False(t, present)
}
