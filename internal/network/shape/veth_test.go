// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// recordingTCRunner records each tc operation as an ordered, human-readable line
// so tests can assert the exact §5.1 install sequence. An operation whose line
// prefix is in failOn returns an error, to exercise mid-sequence failure.
type recordingTCRunner struct {
	calls  []string
	failOn map[string]bool
}

func (r *recordingTCRunner) record(line string) error {
	r.calls = append(r.calls, line)
	verb := strings.SplitN(line, " ", 2)[0]
	if r.failOn[verb] {
		return fmt.Errorf("forced failure on %q", verb)
	}
	return nil
}

func (r *recordingTCRunner) ClassChange(_ context.Context, nic, minor, rate, ceil string, prio int) error {
	return r.record(fmt.Sprintf("class-change %s 1:%s %s %s prio=%d", nic, minor, rate, ceil, prio))
}
func (r *recordingTCRunner) QdiscDelRoot(_ context.Context, nic string) error {
	return r.record(fmt.Sprintf("qdisc-del-root %s", nic))
}
func (r *recordingTCRunner) QdiscAddRoot(_ context.Context, nic, defaultMinor string) error {
	return r.record(fmt.Sprintf("qdisc-add-root %s default=1:%s", nic, defaultMinor))
}
func (r *recordingTCRunner) ClassAddRoot(_ context.Context, nic, rate, ceil string) error {
	return r.record(fmt.Sprintf("class-add-root %s 1:1 %s %s", nic, rate, ceil))
}
func (r *recordingTCRunner) ClassAdd(_ context.Context, nic, minor, rate, ceil string, prio int) error {
	return r.record(fmt.Sprintf("class-add %s 1:%s %s %s prio=%d", nic, minor, rate, ceil, prio))
}
func (r *recordingTCRunner) QdiscAddFqCodel(_ context.Context, nic, minor, handle string) error {
	return r.record(fmt.Sprintf("qdisc-fqcodel %s 1:%s handle=%s", nic, minor, handle))
}

func newRecordingManager(t *testing.T, tc TCRunner) *Manager {
	t.Helper()
	return NewManagerWithConfig(Config{
		LockPath: filepath.Join(t.TempDir(), ".tc-applying"),
		TCRunner: tc,
	})
}

// ingressClasses returns the three §5.1 ingress classes in the sorted order
// loadClassesForDir would yield (alphabetical by name).
func ingressClasses() []*ClassConfig {
	return []*ClassConfig{
		{Name: "backfill-response", Rate: "100mbit", Ceil: "1gbit", Prio: 7},
		{Name: "publisher", Rate: "800mbit", Ceil: "1gbit", Prio: 0},
		{Name: "reserve-ingress", Rate: "100mbit", Ceil: "1gbit", Prio: 1},
	}
}

func TestApplyVethHierarchy_InstallsSection51Sequence(t *testing.T) {
	tc := &recordingTCRunner{}
	m := newRecordingManager(t, tc)

	// default class reserve-ingress → minor 30.
	if err := m.applyVethHierarchy(context.Background(), "lxc1a2b3c", "30", "1gbit", ingressClasses()); err != nil {
		t.Fatalf("applyVethHierarchy: %v", err)
	}

	want := []string{
		"qdisc-del-root lxc1a2b3c",
		"qdisc-add-root lxc1a2b3c default=1:30",
		"class-add-root lxc1a2b3c 1:1 1gbit 1gbit",
		// backfill-response → 1:20 / handle 120
		"class-add lxc1a2b3c 1:20 100mbit 1gbit prio=7",
		"qdisc-fqcodel lxc1a2b3c 1:20 handle=120",
		// publisher → 1:10 / handle 110
		"class-add lxc1a2b3c 1:10 800mbit 1gbit prio=0",
		"qdisc-fqcodel lxc1a2b3c 1:10 handle=110",
		// reserve-ingress → 1:30 / handle 130
		"class-add lxc1a2b3c 1:30 100mbit 1gbit prio=1",
		"qdisc-fqcodel lxc1a2b3c 1:30 handle=130",
	}
	if len(tc.calls) != len(want) {
		t.Fatalf("call count = %d, want %d\ngot:  %v\nwant: %v", len(tc.calls), len(want), tc.calls, want)
	}
	for i := range want {
		if tc.calls[i] != want[i] {
			t.Errorf("call[%d] = %q, want %q", i, tc.calls[i], want[i])
		}
	}
}

func TestApplyVethHierarchy_DefaultCeilFallsBackToRate(t *testing.T) {
	tc := &recordingTCRunner{}
	m := newRecordingManager(t, tc)

	// A class with an empty Ceil should render ceil == rate.
	classes := []*ClassConfig{{Name: "publisher", Rate: "800mbit", Ceil: "", Prio: 0}}
	if err := m.applyVethHierarchy(context.Background(), "lxc9", "30", "1gbit", classes); err != nil {
		t.Fatalf("applyVethHierarchy: %v", err)
	}
	found := false
	for _, c := range tc.calls {
		if c == "class-add lxc9 1:10 800mbit 800mbit prio=0" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ceil to default to rate (800mbit); calls: %v", tc.calls)
	}
}

func TestApplyVethHierarchy_StopsOnMidSequenceFailure(t *testing.T) {
	tc := &recordingTCRunner{failOn: map[string]bool{"class-add": true}}
	m := newRecordingManager(t, tc)

	err := m.applyVethHierarchy(context.Background(), "lxc1", "30", "1gbit", ingressClasses())
	if err == nil {
		t.Fatal("expected error when a class-add fails, got nil")
	}
	// Should have run del-root, add-root, class-add-root, then the first leaf
	// class-add (which fails) — and stopped before its fq_codel.
	if got := tc.calls[len(tc.calls)-1]; !strings.HasPrefix(got, "class-add ") {
		t.Errorf("expected to stop on the failing class-add, last call = %q", got)
	}
	for _, c := range tc.calls {
		if strings.HasPrefix(c, "qdisc-fqcodel") {
			t.Errorf("must not proceed to fq_codel after a class-add failure; calls: %v", tc.calls)
		}
	}
}

func TestEffectiveIngressRootRate(t *testing.T) {
	// Concrete ingress rate is used as-is.
	got, err := effectiveIngressRootRate("500mbit", "1gbit")
	require.NoError(t, err)
	require.Equal(t, "500mbit", got)

	// Non-concrete ingress rate ("auto" has no meaning for a per-pod veth) mirrors
	// the egress rate.
	got, err = effectiveIngressRootRate("auto", "1gbit")
	require.NoError(t, err)
	require.Equal(t, "1gbit", got)

	// Empty ingress rate also falls back to egress.
	got, err = effectiveIngressRootRate("", "800mbit")
	require.NoError(t, err)
	require.Equal(t, "800mbit", got)

	// Neither concrete → error.
	_, err = effectiveIngressRootRate("auto", "")
	require.Error(t, err)
}

func TestValidateVethName(t *testing.T) {
	for _, ok := range []string{"lxc1a2b3c4d", "eth0", "veth_abc", "a"} {
		if err := validateVethName(ok); err != nil {
			t.Errorf("validateVethName(%q): unexpected error %v", ok, err)
		}
	}
	for _, bad := range []string{"", "lxc; rm -rf /", "veth abc", "toolonginterfacename", "lxc/../etc"} {
		if err := validateVethName(bad); err == nil {
			t.Errorf("validateVethName(%q): expected error, got nil", bad)
		}
	}
}

func TestRemoveIngressVeth_BestEffortDelRoot(t *testing.T) {
	tc := &recordingTCRunner{}
	m := newRecordingManager(t, tc)

	if err := m.RemoveIngressVeth(context.Background(), "lxc1a2b3c"); err != nil {
		t.Fatalf("RemoveIngressVeth: %v", err)
	}
	if len(tc.calls) != 1 || tc.calls[0] != "qdisc-del-root lxc1a2b3c" {
		t.Errorf("expected a single qdisc-del-root call, got %v", tc.calls)
	}
}

func TestRemoveIngressVeth_RejectsBadName(t *testing.T) {
	tc := &recordingTCRunner{}
	m := newRecordingManager(t, tc)
	if err := m.RemoveIngressVeth(context.Background(), "bad name"); err == nil {
		t.Error("expected error for invalid veth name, got nil")
	}
	if len(tc.calls) != 0 {
		t.Errorf("expected no tc calls for an invalid name, got %v", tc.calls)
	}
}
