// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package reassert

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/automa-saga/logx"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/daemon/privexec"
	ra "github.com/hashgraph/solo-weaver/internal/network/reassert"
)

// The daemon execs this once a minute; without the opt-out it would also run
// the install workflow and migrations against /usr/lib, read-only under ProtectSystem=strict.
func TestReassertCmd_SkipsGlobalChecks(t *testing.T) {
	assert.False(t, common.RequireGlobalChecks(reassertCmd),
		"network reassert must opt out of the global pre-run")
}

// TestReassertCmd_DeclaresNoLocalOutputFlag guards against the trap `network
// firewall show` fell into: a local --output breaks common.OutputIsJSON().
func TestReassertCmd_DeclaresNoLocalOutputFlag(t *testing.T) {
	assert.Nil(t, reassertCmd.Flags().Lookup("output"),
		"reassert must use the root persistent --output, not shadow it")
}

func TestReassertCmd_HasCheckFlag(t *testing.T) {
	f := reassertCmd.Flags().Lookup("check")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}

// canonicalReport is what the engine emits on every run: the three artifacts in
// probe order, once each, with every optional field exercised across them.
func canonicalReport() ra.Report {
	return ra.Report{Type: ra.ReportType, Artifacts: []ra.ArtifactStatus{
		{
			Artifact: ra.ArtifactHostFirewall,
			Expected: true, Present: true,
			Reasserted: true, Recovered: true,
			Detail: "restored",
		},
		{
			// The two nft artifacts share one lock; this is that lock held.
			Artifact: ra.ArtifactWorkloadPolicy,
			Skipped:  true,
			Detail:   "skipped: an apply is in progress",
		},
		{
			Artifact: ra.ArtifactEgressQdisc,
			Expected: true, ProbeFailed: true,
			Detail: "nic=eth0; cannot determine: no such device",
		},
	}}
}

// TestJSONContract_MatchesWhatTheDaemonParses is the drift guard: privexec
// declares its own result struct instead of importing the engine.
func TestJSONContract_MatchesWhatTheDaemonParses(t *testing.T) {
	emitted := canonicalReport()

	raw, err := json.Marshal(emitted)
	require.NoError(t, err)

	// Through the daemon's own selector, so the tag constants are pinned together.
	parsed, err := privexec.ParseNetworkReassertReport(raw)
	require.NoError(t, err)
	require.Equal(t, ra.ReportType, parsed.Type)
	require.Len(t, parsed.Artifacts, len(emitted.Artifacts))

	for i, want := range emitted.Artifacts {
		got := parsed.Artifacts[i]
		assert.Equal(t, want.Artifact, got.Artifact)
		assert.Equal(t, want.Expected, got.Expected)
		assert.Equal(t, want.Present, got.Present)
		assert.Equal(t, want.ProbeFailed, got.ProbeFailed)
		assert.Equal(t, want.Skipped, got.Skipped, "artifact %d", i)
		assert.Equal(t, want.Reasserted, got.Reasserted)
		assert.Equal(t, want.Recovered, got.Recovered)
		assert.Equal(t, want.Detail, got.Detail)
	}
}

// TestJSONContract_DaemonRejectsMalformedArtifactSets pins the other half: the
// daemon indexes by artifact name, so only the three names once each may parse.
func TestJSONContract_DaemonRejectsMalformedArtifactSets(t *testing.T) {
	for name, mutate := range map[string]func([]ra.ArtifactStatus) []ra.ArtifactStatus{
		"duplicate artifact": func(a []ra.ArtifactStatus) []ra.ArtifactStatus {
			a[2].Artifact = a[0].Artifact
			return a
		},
		"missing artifact": func(a []ra.ArtifactStatus) []ra.ArtifactStatus {
			return a[:2]
		},
		"unknown artifact": func(a []ra.ArtifactStatus) []ra.ArtifactStatus {
			a[1].Artifact = "ingress-qdisc"
			return a
		},
		"extra artifact": func(a []ra.ArtifactStatus) []ra.ArtifactStatus {
			return append(a, ra.ArtifactStatus{Artifact: ra.ArtifactEgressQdisc})
		},
	} {
		t.Run(name, func(t *testing.T) {
			report := canonicalReport()
			report.Artifacts = mutate(report.Artifacts)

			raw, err := json.Marshal(report)
			require.NoError(t, err)

			_, err = privexec.ParseNetworkReassertReport(raw)
			require.Error(t, err, "the daemon must reject a report it cannot index by artifact")
		})
	}
}

func TestReassertState(t *testing.T) {
	for name, tc := range map[string]struct {
		in   ra.ArtifactStatus
		want string
	}{
		"absent from this host": {ra.ArtifactStatus{}, "not-provisioned"},
		// Skipped outranks the unset Expected it necessarily carries: the run never
		// looked, so "not-provisioned" would be a false report.
		"skipped for a held lock": {ra.ArtifactStatus{Skipped: true}, "skipped"},
		"healthy":                 {ra.ArtifactStatus{Expected: true, Present: true}, "present"},
		"missing":                 {ra.ArtifactStatus{Expected: true}, "MISSING"},
		"restored": {ra.ArtifactStatus{
			Expected: true, Reasserted: true, Recovered: true,
		}, "restored"},
		"repair did not work": {ra.ArtifactStatus{
			Expected: true, Reasserted: true,
		}, "UNRECOVERED"},
		// A probe failure outranks presence: a "present" read taken from a failed
		// probe means nothing.
		"unprobeable": {ra.ArtifactStatus{
			Expected: true, Present: true, ProbeFailed: true,
		}, "unknown"},
		// A lock fault leaves Expected unset; "not-provisioned" would be a false claim.
		"unprobeable before provisioning was read": {ra.ArtifactStatus{
			ProbeFailed: true,
		}, "unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, reassertState(tc.in))
		})
	}
}

func TestWriteReassertTable_ReportsEveryArtifact(t *testing.T) {
	// Including the ones this host does not have: an operator reading --check
	// should see a plane is absent, not have it silently missing from the list.
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	writeReassertTable(cmd, ra.Report{Artifacts: []ra.ArtifactStatus{
		{Artifact: ra.ArtifactHostFirewall, Expected: true, Present: true},
		{Artifact: ra.ArtifactWorkloadPolicy},
		{Artifact: ra.ArtifactEgressQdisc, Expected: true, Detail: "nic=eth0"},
	}})

	out := buf.String()
	assert.Contains(t, out, "ARTIFACT")
	assert.Contains(t, out, ra.ArtifactHostFirewall)
	assert.Contains(t, out, ra.ArtifactWorkloadPolicy)
	assert.Contains(t, out, "not-provisioned")
	assert.Contains(t, out, "MISSING")
	assert.Contains(t, out, "nic=eth0")
}

// TestEmitReassertReport_JSONReportIsSelectableAmongLogLines pins tag selection
// even with logs and the report merged into one stream (e.g. `2>&1`).
func TestEmitReassertReport_JSONReportIsSelectableAmongLogLines(t *testing.T) {
	origFormat := common.OutputFormat
	common.OutputFormat = "json"
	t.Cleanup(func() { common.OutputFormat = origFormat })

	origLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(origLevel) })

	var stream bytes.Buffer
	origLogger := *logx.As()
	logx.SetLogger(zerolog.New(&stream))
	t.Cleanup(func() { logx.SetLogger(origLogger) })

	// A restored artifact takes the WARN path, the report the daemon most needs.
	report := ra.Report{Type: ra.ReportType, Artifacts: []ra.ArtifactStatus{
		{
			Artifact: ra.ArtifactHostFirewall,
			Expected: true, Reasserted: true, Recovered: true,
		},
		{Artifact: ra.ArtifactWorkloadPolicy, Expected: true, Present: true},
		{Artifact: ra.ArtifactEgressQdisc},
	}}

	cmd := &cobra.Command{}
	cmd.SetOut(&stream)
	require.NoError(t, emitReassertReport(cmd, report, false))

	out := stream.String()
	assert.Contains(t, out, "re-asserted", "the WARN must still be emitted")

	parsed, err := privexec.ParseNetworkReassertReport(stream.Bytes())
	require.NoError(t, err, "the daemon must find the tagged report in: %q", out)
	require.Len(t, parsed.Artifacts, 3)
	assert.True(t, parsed.Artifacts[0].Recovered)

	// The report is exactly one line, so a line-oriented consumer never sees a
	// fragment of it.
	var reportLines int
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(line, `{"type":"reassert"`) {
			reportLines++
		}
	}
	assert.Equal(t, 1, reportLines, "got: %q", out)
}

// --- journal summary -------------------------------------------------------

// captureLogs points logx at a buffer at DEBUG level, so even the quietest
// branch actually emits a line rather than passing the assertion vacuously.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	origLevel := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	t.Cleanup(func() { zerolog.SetGlobalLevel(origLevel) })

	var buf bytes.Buffer
	origLogger := *logx.As()
	logx.SetLogger(zerolog.New(&buf))
	t.Cleanup(func() { logx.SetLogger(origLogger) })

	return &buf
}

// soleLogLine decodes the one event the summary emitted.
func soleLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	lines := make([]string, 0, 2)
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	require.Len(t, lines, 1, "the summary must be exactly one journal line, got: %q", buf.String())

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &m), "not JSON: %q", lines[0])
	return m
}

func present(id string) ra.ArtifactStatus {
	return ra.ArtifactStatus{Artifact: id, Expected: true, Present: true}
}

// TestLogReassertOutcome_OneLinePerOutcome pins every branch of the journal
// summary. The daemon's operators read these lines, so a run that repaired
// nothing must not look like one that repaired something, and vice versa.
func TestLogReassertOutcome_OneLinePerOutcome(t *testing.T) {
	restored := ra.ArtifactStatus{
		Artifact: ra.ArtifactHostFirewall, Expected: true,
		Present: true, Reasserted: true, Recovered: true,
	}
	unrecovered := ra.ArtifactStatus{
		Artifact: ra.ArtifactWorkloadPolicy, Expected: true, Reasserted: true,
	}
	missing := ra.ArtifactStatus{Artifact: ra.ArtifactEgressQdisc, Expected: true}
	skipped := ra.ArtifactStatus{
		Artifact: ra.ArtifactWorkloadPolicy, Skipped: true,
		Detail: "skipped: an apply is in progress",
	}

	for name, tc := range map[string]struct {
		artifacts []ra.ArtifactStatus
		checkOnly bool
		wantLevel string
		wantMsg   string
	}{
		"a repair that did not work is an error": {
			artifacts: []ra.ArtifactStatus{unrecovered, present(ra.ArtifactEgressQdisc)},
			wantLevel: "error",
			wantMsg:   "could not be fully re-asserted",
		},
		"outstanding damage found by --check": {
			artifacts: []ra.ArtifactStatus{missing, present(ra.ArtifactHostFirewall)},
			checkOnly: true,
			wantLevel: "warn",
			wantMsg:   "re-run without --check to restore it",
		},
		"a successful repair warns, because something destroyed live state": {
			artifacts: []ra.ArtifactStatus{restored, present(ra.ArtifactEgressQdisc)},
			wantLevel: "warn",
			wantMsg:   "has been re-asserted",
		},
		"a skipped plane was not verified": {
			artifacts: []ra.ArtifactStatus{skipped, present(ra.ArtifactHostFirewall)},
			wantLevel: "info",
			wantMsg:   "was not verified",
		},
		"a clean --check says so out loud": {
			artifacts: []ra.ArtifactStatus{present(ra.ArtifactHostFirewall)},
			checkOnly: true,
			wantLevel: "info",
			wantMsg:   "nothing missing",
		},
		"a clean unattended run stays quiet at debug": {
			artifacts: []ra.ArtifactStatus{present(ra.ArtifactHostFirewall)},
			wantLevel: "debug",
			wantMsg:   "nothing to re-assert",
		},
	} {
		t.Run(name, func(t *testing.T) {
			buf := captureLogs(t)

			logReassertOutcome(ra.Report{Type: ra.ReportType, Artifacts: tc.artifacts}, tc.checkOnly)

			got := soleLogLine(t, buf)
			assert.Equal(t, tc.wantLevel, got["level"])
			assert.Contains(t, got["message"], tc.wantMsg)
		})
	}
}

// TestLogReassertOutcome_UnhealthyOutranksARestoreInTheSameRun: one plane came
// back and another did not. The line must be the error, and it must still name
// what was restored, or an operator chasing the failure loses that context.
func TestLogReassertOutcome_UnhealthyOutranksARestoreInTheSameRun(t *testing.T) {
	buf := captureLogs(t)

	logReassertOutcome(ra.Report{Type: ra.ReportType, Artifacts: []ra.ArtifactStatus{
		{
			Artifact: ra.ArtifactHostFirewall, Expected: true,
			Present: true, Reasserted: true, Recovered: true,
		},
		{Artifact: ra.ArtifactEgressQdisc, Expected: true, Reasserted: true},
	}}, false)

	got := soleLogLine(t, buf)
	assert.Equal(t, "error", got["level"])
	assert.Equal(t, []any{ra.ArtifactEgressQdisc}, got["unhealthy"])
	assert.Equal(t, []any{ra.ArtifactHostFirewall}, got["restored"],
		"the successful repair must still be reported alongside the failure")
}

// TestLogReassertOutcome_SkippedNeverReadsAsVerified is the operator-trust
// invariant: a plane nobody looked at must not produce the quiet "verified" line.
func TestLogReassertOutcome_SkippedNeverReadsAsVerified(t *testing.T) {
	buf := captureLogs(t)

	logReassertOutcome(ra.Report{Type: ra.ReportType, Artifacts: []ra.ArtifactStatus{
		{Artifact: ra.ArtifactHostFirewall, Skipped: true},
		{Artifact: ra.ArtifactWorkloadPolicy, Skipped: true},
		present(ra.ArtifactEgressQdisc),
	}}, false)

	got := soleLogLine(t, buf)
	assert.NotContains(t, got["message"], "nothing to re-assert")
	assert.Equal(t, []any{ra.ArtifactHostFirewall, ra.ArtifactWorkloadPolicy}, got["skipped"])
}

// canonicalChecks exercises every field of the checks section.
func canonicalChecks() *ra.Checks {
	return &ra.Checks{
		Rules: ra.RulesCheck{
			Status: ra.CheckFailed, Detail: "differ", LiveFormat: 2,
			Missing: []string{"rule a"}, Unexpected: []string{"rule b"},
		},
		Filters: ra.FiltersCheck{Status: ra.CheckFailed, Detail: "d", Devices: []ra.DeviceFilters{
			{Dev: "lxc1", Role: ra.RoleVeth, Filters: []string{"parent 1: pref 1 protocol all kind u32"}},
			{Dev: "eth0", Role: ra.RoleEgress, Error: "tc died"},
		}},
		Counters: &ra.CounterSample{
			RulesEpoch: "7", ForwardCounted: true, ForwardBytes: 9000,
			RuleBytes: map[string]uint64{"partner": 100},
			Devices: []ra.DeviceCounters{{
				Dev: "eth0", Role: ra.RoleEgress, IfIndex: 2, DefaultClass: "reserve-egress",
				TrunkBytes: 1000, LaneBytes: map[string]uint64{"partner": 400}, Error: "e",
			}},
		},
	}
}

// TestJSONContract_ChecksSurviveTheDaemonsParse is the drift guard for the
// checks section, which privexec also mirrors instead of importing.
func TestJSONContract_ChecksSurviveTheDaemonsParse(t *testing.T) {
	emitted := canonicalReport()
	emitted.Checks = canonicalChecks()

	raw, err := json.Marshal(emitted)
	require.NoError(t, err)
	parsed, err := privexec.ParseNetworkReassertReport(raw)
	require.NoError(t, err)
	require.NotNil(t, parsed.Checks)

	// Re-encode both sides; the same JSON means every field made it across.
	want, err := json.Marshal(emitted.Checks)
	require.NoError(t, err)
	got, err := json.Marshal(parsed.Checks)
	require.NoError(t, err)
	var wantDoc, gotDoc map[string]any
	require.NoError(t, json.Unmarshal(want, &wantDoc))
	require.NoError(t, json.Unmarshal(got, &gotDoc))
	assert.Equal(t, wantDoc, pruneEmpty(gotDoc))
}

// pruneEmpty drops empty values, which the engine omits and privexec keeps.
func pruneEmpty(v any) map[string]any {
	var walk func(any) any
	walk = func(v any) any {
		switch t := v.(type) {
		case map[string]any:
			out := map[string]any{}
			for k, val := range t {
				val = walk(val)
				switch x := val.(type) {
				case nil:
					continue
				case string:
					if x == "" {
						continue
					}
				case float64:
					if x == 0 {
						continue
					}
				case []any:
					if len(x) == 0 {
						continue
					}
				}
				out[k] = val
			}
			return out
		case []any:
			out := make([]any, len(t))
			for i, val := range t {
				out[i] = walk(val)
			}
			return out
		default:
			return v
		}
	}
	return walk(v).(map[string]any)
}

// TestJSONContract_CheckConstantsMatch pins the values privexec mirrors.
func TestJSONContract_CheckConstantsMatch(t *testing.T) {
	assert.Equal(t, ra.CheckOK, privexec.NetworkCheckOK)
	assert.Equal(t, ra.CheckFailed, privexec.NetworkCheckFailed)
	assert.Equal(t, ra.CheckOlderFormat, privexec.NetworkCheckOlderFormat)
	assert.Equal(t, ra.CheckUnknown, privexec.NetworkCheckUnknown)
	assert.Equal(t, ra.CheckNotChecked, privexec.NetworkCheckNotChecked)
	assert.Equal(t, ra.CheckSkipped, privexec.NetworkCheckSkipped)
	assert.Equal(t, ra.RoleEgress, privexec.NetworkRoleEgress)
	assert.Equal(t, ra.RoleVeth, privexec.NetworkRoleVeth)
}

func TestWriteReassertTable_ShowsChecksAndTheirLines(t *testing.T) {
	report := canonicalReport()
	report.Checks = canonicalChecks()
	cmd := GetCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)

	writeReassertTable(cmd, report)

	s := out.String()
	assert.Contains(t, s, "CHECK")
	assert.Contains(t, s, "- rule a")
	assert.Contains(t, s, "+ rule b")
	assert.Contains(t, s, "lxc1 (veth): parent 1: pref 1 protocol all kind u32")
	assert.Contains(t, s, "eth0 (egress): tc died")
}

// filterRows returns the indented device lines of the CHECK table, trimmed.
func filterRows(out string) []string {
	var rows []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, " ") && strings.Contains(l, "): ") {
			rows = append(rows, strings.TrimSpace(l))
		}
	}
	return rows
}

func TestWriteReassertTable_FilterRowsPerDevice(t *testing.T) {
	report := ra.Report{Checks: &ra.Checks{
		Rules: ra.RulesCheck{Status: ra.CheckOK},
		Filters: ra.FiltersCheck{Status: ra.CheckFailed, Devices: []ra.DeviceFilters{
			{Dev: "lxc1", Role: ra.RoleVeth, Filters: []string{"f1", "f2"}, Error: "boom"},
			{Dev: "lxc2", Role: ra.RoleVeth},
			{Dev: "eth0", Role: ra.RoleEgress, Error: "tc died"},
			{Dev: "lxc3", Role: ra.RoleVeth, Filters: []string{"f3"}},
		}},
	}}
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)

	writeReassertTable(cmd, report)

	assert.Equal(t, []string{
		"lxc1 (veth): f1",
		"lxc1 (veth): f2",
		"lxc1 (veth): boom",
		"eth0 (egress): tc died",
		"lxc3 (veth): f3",
	}, filterRows(out.String()), "filters first, then the error; a clean device prints nothing")
}

// The error line must not be appended into the caller's Filters slice when it
// has spare capacity.
func TestWriteReassertTable_DoesNotWriteIntoTheReport(t *testing.T) {
	filters := make([]string, 1, 4)
	filters[0] = "f1"
	report := ra.Report{Checks: &ra.Checks{Filters: ra.FiltersCheck{Devices: []ra.DeviceFilters{
		{Dev: "lxc1", Role: ra.RoleVeth, Filters: filters, Error: "boom"},
	}}}}
	cmd := &cobra.Command{}
	var first, second bytes.Buffer

	cmd.SetOut(&first)
	writeReassertTable(cmd, report)
	cmd.SetOut(&second)
	writeReassertTable(cmd, report)

	assert.Equal(t, []string{"f1"}, report.Checks.Filters.Devices[0].Filters)
	assert.Empty(t, filters[:2][1], "the backing array past len must stay untouched")
	assert.Equal(t, first.String(), second.String())
}
