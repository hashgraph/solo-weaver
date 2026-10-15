// SPDX-License-Identifier: Apache-2.0

package reassert

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/internal/network/shape"
)

// listing is a minimal `nft -j list table` document: one tagged forward rule and
// one stamp rule per class in bytes.
func listing(handle int, format string, match string, bytes map[string]uint64) []byte {
	objs := []string{
		fmt.Sprintf(`{"table":{"family":"inet","name":"weaver-workload-policy","handle":%d}}`, handle),
		`{"chain":{"family":"inet","table":"weaver-workload-policy","name":"forward","type":"filter","hook":"forward","prio":0,"policy":"accept"}}`,
	}
	fwd := `{"rule":{"family":"inet","table":"weaver-workload-policy","chain":"forward","expr":[{"counter":{"packets":1,"bytes":9000}}]`
	if format != "" {
		fwd += `,"comment":"weaver:format=` + format + `"`
	}
	objs = append(objs, fwd+`}}`)
	for _, class := range []string{"partner", "publisher"} {
		objs = append(objs, fmt.Sprintf(
			`{"rule":{"family":"inet","table":"weaver-workload-policy","chain":"forward_ipv4","comment":"weaver:class=%s","expr":[%s,{"counter":{"packets":1,"bytes":%d}},{"accept":null}]}}`,
			class, match, bytes[class]))
	}
	return []byte(`{"nftables":[` + strings.Join(objs, ",") + `]}`)
}

const (
	matchGood = `{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"10.4.0.0/24"}}`
	matchEdit = `{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"10.9.9.0/24"}}`
)

// fakeVerifier returns a verifier whose kernel is healthy; tests break one seam.
func fakeVerifier() *verifier {
	return &verifier{
		expectedRules: func(context.Context) (string, error) { return "doc", nil },
		liveRules: func(context.Context) ([]byte, error) {
			return listing(7, "2", matchGood, map[string]uint64{"partner": 100, "publisher": 200}), nil
		},
		scratchRules: func(context.Context, string) ([]byte, error) {
			return listing(1, "2", matchGood, nil), nil
		},
		reloadRules: func(context.Context, string) error { return errors.New("reload not expected") },
		canPersist:  func() error { return nil },
		laneTrees: func(context.Context) ([]shape.LaneTree, error) {
			return []shape.LaneTree{
				{Dev: "eth0", DefaultClass: "reserve-egress"},
				{Dev: "lxc1", DefaultClass: "reserve-ingress"},
			}, nil
		},
		treeFilters: func(context.Context, string) ([]string, error) { return nil, nil },
		classStats: func(_ context.Context, dev string) (map[string]shape.ClassStat, error) {
			if dev == "eth0" {
				return map[string]shape.ClassStat{
					"1:1": {Bytes: 1000}, "1:40": {Bytes: 400}, "1:60": {Bytes: 600},
				}, nil
			}
			return map[string]shape.ClassStat{"1:1": {Bytes: 50}, "1:10": {Bytes: 50}, "1:ff": {Bytes: 1}}, nil
		},
		ifIndex: func(dev string) (int, error) { return len(dev), nil },
	}
}

var liveScope = verifyScope{policyLive: true, egressNIC: "eth0", shapedNIC: "eth0"}

func TestVerify_HealthyHostPassesEveryCheck(t *testing.T) {
	c := fakeVerifier().verify(context.Background(), liveScope)

	assert.Equal(t, CheckOK, c.Rules.Status)
	assert.Equal(t, 2, c.Rules.LiveFormat)
	assert.Equal(t, CheckOK, c.Filters.Status)
	assert.Empty(t, c.Filters.Detail)
	require.Len(t, c.Filters.Devices, 2)
	assert.Equal(t, RoleEgress, c.Filters.Devices[0].Role)
	assert.Equal(t, RoleVeth, c.Filters.Devices[1].Role)
}

func TestVerify_SamplesRuleAndLaneCounters(t *testing.T) {
	s := fakeVerifier().verify(context.Background(), liveScope).Counters
	require.NotNil(t, s)

	assert.Equal(t, "7", s.RulesEpoch)
	assert.True(t, s.ForwardCounted)
	assert.Equal(t, uint64(9000), s.ForwardBytes)
	assert.Equal(t, map[string]uint64{"partner": 100, "publisher": 200}, s.RuleBytes)
	require.Len(t, s.Devices, 2)
	eth := s.Devices[0]
	assert.Equal(t, DeviceCounters{
		Dev: "eth0", Role: RoleEgress, IfIndex: 4, DefaultClass: "reserve-egress",
		TrunkBytes: 1000, LaneBytes: map[string]uint64{"partner": 400, "reserve-egress": 600},
	}, eth)
	assert.Equal(t, map[string]uint64{"publisher": 50}, s.Devices[1].LaneBytes, "unknown classids are dropped")
}

func TestVerify_HandEditedRuleIsDriftWithTheDifferingLines(t *testing.T) {
	v := fakeVerifier()
	v.liveRules = func(context.Context) ([]byte, error) { return listing(7, "2", matchEdit, nil), nil }

	rc := v.verify(context.Background(), liveScope).Rules

	assert.Equal(t, CheckFailed, rc.Status)
	require.Len(t, rc.Missing, 2)
	require.Len(t, rc.Unexpected, 2)
	assert.Contains(t, rc.Missing[0], "10.4.0.0/24")
	assert.Contains(t, rc.Unexpected[0], "10.9.9.0/24")
}

func TestVerify_ReorderedRulesAreDriftWithoutLines(t *testing.T) {
	v := fakeVerifier()
	swap := strings.NewReplacer("class=partner", "class=publisher", "class=publisher", "class=partner")
	v.liveRules = func(context.Context) ([]byte, error) {
		return []byte(swap.Replace(string(listing(7, "2", matchGood, nil)))), nil
	}

	rc := v.verify(context.Background(), liveScope).Rules

	assert.Equal(t, CheckFailed, rc.Status)
	assert.Contains(t, rc.Detail, "different order")
	assert.Empty(t, rc.Missing)
	assert.Empty(t, rc.Unexpected)
}

func TestVerify_OlderFormatIsReportedCalmly(t *testing.T) {
	v := fakeVerifier()
	v.liveRules = func(context.Context) ([]byte, error) { return listing(7, "", matchGood, nil), nil }

	rc := v.verify(context.Background(), liveScope).Rules

	assert.Equal(t, CheckOlderFormat, rc.Status)
	assert.Equal(t, 1, rc.LiveFormat)
	assert.Empty(t, rc.Missing, "an older format is not drift")
}

// TestVerify_HandEditOnAnOlderFormatTableIsStillDrift: a format difference must not hide hand edits.
func TestVerify_HandEditOnAnOlderFormatTableIsStillDrift(t *testing.T) {
	v := fakeVerifier()
	v.liveRules = func(context.Context) ([]byte, error) { return listing(7, "", matchEdit, nil), nil }

	rc := v.verify(context.Background(), liveScope).Rules

	assert.Equal(t, CheckFailed, rc.Status)
	assert.Equal(t, 1, rc.LiveFormat)
	require.NotEmpty(t, rc.Unexpected)
	assert.Contains(t, rc.Unexpected[0], "10.9.9.0/24")
}

var refreshScope = verifyScope{policyLive: true, egressNIC: "eth0", shapedNIC: "eth0", refreshOlderFormat: true}

// olderTableVerifier serves a format-1 live table until reload succeeds, then
// afterFormat. reloads records every document passed to reload.
func olderTableVerifier(afterFormat string, reloadErr error) (*verifier, *[]string) {
	v := fakeVerifier()
	var reloads []string
	reloaded := false
	v.reloadRules = func(_ context.Context, doc string) error {
		reloads = append(reloads, doc)
		if reloadErr != nil {
			return reloadErr
		}
		reloaded = true
		return nil
	}
	v.liveRules = func(context.Context) ([]byte, error) {
		if reloaded {
			return listing(8, afterFormat, matchGood, map[string]uint64{"partner": 0, "publisher": 0}), nil
		}
		return listing(7, "", matchGood, nil), nil
	}
	return v, &reloads
}

func TestVerify_FullRunReloadsAnOlderFormatTable(t *testing.T) {
	v, reloads := olderTableVerifier("2", nil)

	c := v.verify(context.Background(), refreshScope)

	assert.Equal(t, []string{"doc"}, *reloads, "the document just compared is the one loaded")
	assert.Equal(t, CheckOK, c.Rules.Status)
	assert.Equal(t, 2, c.Rules.LiveFormat)
	assert.Equal(t, "reloaded the live rules from render format 1 to 2", c.Rules.Detail)
	assert.Equal(t, 1, c.Rules.ReloadedFrom)
	require.NotNil(t, c.Counters)
	assert.Equal(t, "8", c.Counters.RulesEpoch, "counters come from the reloaded table")
	assert.True(t, c.Counters.ForwardCounted)
}

func TestVerify_CheckNeverReloads(t *testing.T) {
	v, reloads := olderTableVerifier("2", nil)

	rc := v.verify(context.Background(), liveScope).Rules

	assert.Empty(t, *reloads)
	assert.Equal(t, CheckOlderFormat, rc.Status)
	assert.Contains(t, rc.Detail, "without --check reloads them")
}

// TestVerify_NewerFormatIsNeverReloaded: an older binary must not overwrite a newer table.
func TestVerify_NewerFormatIsNeverReloaded(t *testing.T) {
	v := fakeVerifier()
	reloaded := false
	v.reloadRules = func(context.Context, string) error { reloaded = true; return nil }
	v.liveRules = func(context.Context) ([]byte, error) { return listing(7, "3", matchGood, nil), nil }

	rc := v.verify(context.Background(), refreshScope).Rules

	assert.False(t, reloaded)
	assert.Equal(t, CheckOlderFormat, rc.Status)
	assert.Contains(t, rc.Detail, "network policy create")
}

func TestVerify_HandEditOnAnOlderFormatTableIsNotReloaded(t *testing.T) {
	v := fakeVerifier()
	reloaded := false
	v.reloadRules = func(context.Context, string) error { reloaded = true; return nil }
	v.liveRules = func(context.Context) ([]byte, error) { return listing(7, "", matchEdit, nil), nil }

	rc := v.verify(context.Background(), refreshScope).Rules

	assert.False(t, reloaded, "drift is reported, never overwritten")
	assert.Equal(t, CheckFailed, rc.Status)
}

func TestVerify_FailedReloadIsNeverReportedOK(t *testing.T) {
	v, reloads := olderTableVerifier("2", errors.New("nft -f failed: busy"))

	c := v.verify(context.Background(), refreshScope)

	assert.Len(t, *reloads, 1)
	assert.Equal(t, CheckOlderFormat, c.Rules.Status)
	assert.Zero(t, c.Rules.ReloadedFrom)
	assert.Contains(t, c.Rules.Detail, "nft -f failed: busy")
	require.NotNil(t, c.Counters)
	assert.Equal(t, "7", c.Counters.RulesEpoch)
}

// TestVerify_ReloadedButUnsavedIsNeverReportedOK: a failed save reverts on reboot, so it is not ok.
func TestVerify_ReloadedButUnsavedIsNeverReportedOK(t *testing.T) {
	v, _ := olderTableVerifier("2", nil)
	reload := v.reloadRules
	v.reloadRules = func(ctx context.Context, doc string) error {
		_ = reload(ctx, doc)
		return errors.New("persisting /etc/solo-provisioner/weaver.nft failed: no space left on device")
	}

	c := v.verify(context.Background(), refreshScope)

	assert.Equal(t, CheckOlderFormat, c.Rules.Status)
	assert.Equal(t, 2, c.Rules.LiveFormat)
	assert.Zero(t, c.Rules.ReloadedFrom)
	assert.Contains(t, c.Rules.Detail, "no space left on device")
	require.NotNil(t, c.Counters)
	assert.Equal(t, "8", c.Counters.RulesEpoch)
}

// TestVerify_UnsavableTableIsNotReloaded: with a read-only boot copy, tell the operator instead.
func TestVerify_UnsavableTableIsNotReloaded(t *testing.T) {
	v, reloads := olderTableVerifier("2", nil)
	v.canPersist = func() error { return errors.New("/etc/solo-provisioner is not writable") }

	c := v.verify(context.Background(), refreshScope)

	assert.Empty(t, *reloads)
	assert.Equal(t, CheckOlderFormat, c.Rules.Status)
	assert.Zero(t, c.Rules.ReloadedFrom)
	assert.Contains(t, c.Rules.Detail, "not writable")
	assert.Contains(t, c.Rules.Detail, "sudo solo-provisioner network reassert")
	require.NotNil(t, c.Counters)
	assert.Equal(t, "7", c.Counters.RulesEpoch)
}

func TestVerify_ReloadThatDidNotTakeIsStillOlderFormat(t *testing.T) {
	v, _ := olderTableVerifier("", nil)

	rc := v.verify(context.Background(), refreshScope).Rules

	assert.Equal(t, CheckOlderFormat, rc.Status)
	assert.Equal(t, 1, rc.LiveFormat)
}

func TestVerify_UnreadableRulesAfterReloadAreUnknown(t *testing.T) {
	v, _ := olderTableVerifier("2", nil)
	reads := 0
	v.liveRules = func(context.Context) ([]byte, error) {
		reads++
		if reads > 1 {
			return nil, errors.New("nft died")
		}
		return listing(7, "", matchGood, nil), nil
	}
	v.reloadRules = func(context.Context, string) error { return nil }

	c := v.verify(context.Background(), refreshScope)

	assert.Equal(t, CheckUnknown, c.Rules.Status)
	assert.Contains(t, c.Rules.Detail, "after reloading them")
	assert.Nil(t, c.Counters, "the pre-reload counters are stale")
}

// TestVerify_CannotBuildExpectedIsUnknownNeverDrift: no false alarm, and counters still flow.
func TestVerify_CannotBuildExpectedIsUnknownNeverDrift(t *testing.T) {
	for name, mutate := range map[string]func(*verifier){
		"registry unreadable": func(v *verifier) {
			v.expectedRules = func(context.Context) (string, error) { return "", errors.New("registry gone") }
		},
		"scratch load failed": func(v *verifier) {
			v.scratchRules = func(context.Context, string) ([]byte, error) { return nil, errors.New("unshare: EPERM") }
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := fakeVerifier()
			mutate(v)

			c := v.verify(context.Background(), liveScope)

			assert.Equal(t, CheckUnknown, c.Rules.Status)
			assert.Contains(t, c.Rules.Detail, "cannot determine")
			assert.NotNil(t, c.Counters)
		})
	}
}

func TestVerify_UnreadableLiveRulesDropTheCounters(t *testing.T) {
	v := fakeVerifier()
	v.liveRules = func(context.Context) ([]byte, error) { return nil, errors.New("nft died") }

	c := v.verify(context.Background(), liveScope)

	assert.Equal(t, CheckUnknown, c.Rules.Status)
	assert.Nil(t, c.Counters)
}

func TestVerify_PolicyNotLiveSkipsTheRulesCheck(t *testing.T) {
	c := fakeVerifier().verify(context.Background(), verifyScope{egressNIC: "eth0", shapedNIC: "eth0"})

	assert.Equal(t, CheckNotChecked, c.Rules.Status)
	assert.Nil(t, c.Counters)
	assert.Equal(t, CheckOK, c.Filters.Status, "the lanes are still checked")
}

// TestVerify_NICIsNeverTakenForAPodVeth: else the bypass check would fire on host traffic.
func TestVerify_NICIsNeverTakenForAPodVeth(t *testing.T) {
	for name, tc := range map[string]struct {
		scope    verifyScope
		wantEth0 string
		wantLxc1 string
	}{
		"qdisc probe failed": {verifyScope{policyLive: true, shapedNIC: "eth0"}, RoleEgress, RoleVeth},
		"NIC not named":      {verifyScope{policyLive: true, nicUnknown: true}, RoleUnknown, RoleUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			c := fakeVerifier().verify(context.Background(), tc.scope)

			require.Len(t, c.Counters.Devices, 2)
			assert.Equal(t, tc.wantEth0, c.Counters.Devices[0].Role)
			assert.Equal(t, tc.wantLxc1, c.Counters.Devices[1].Role)
			assert.Equal(t, tc.wantEth0, c.Filters.Devices[0].Role)
		})
	}
}

func TestVerify_StrayFilterFailsOnlyItsDevice(t *testing.T) {
	v := fakeVerifier()
	v.treeFilters = func(_ context.Context, dev string) ([]string, error) {
		if dev == "lxc1" {
			return []string{"parent 1: pref 1 protocol all kind u32"}, nil
		}
		return nil, nil
	}

	fc := v.verify(context.Background(), liveScope).Filters

	assert.Equal(t, CheckFailed, fc.Status)
	assert.Empty(t, fc.Devices[0].Filters)
	assert.Equal(t, []string{"parent 1: pref 1 protocol all kind u32"}, fc.Devices[1].Filters)
}

func TestVerify_FilterReadErrorIsUnknownUnlessAStrayWasFound(t *testing.T) {
	v := fakeVerifier()
	v.treeFilters = func(context.Context, string) ([]string, error) { return nil, errors.New("tc died") }
	assert.Equal(t, CheckUnknown, v.verify(context.Background(), liveScope).Filters.Status)

	v.treeFilters = func(_ context.Context, dev string) ([]string, error) {
		if dev == "eth0" {
			return nil, errors.New("tc died")
		}
		return []string{"parent 1:1 pref 1 protocol ip kind fw"}, nil
	}
	assert.Equal(t, CheckFailed, v.verify(context.Background(), liveScope).Filters.Status)
}

func TestVerify_NoVethIsNotedNotFailed(t *testing.T) {
	v := fakeVerifier()
	v.laneTrees = func(context.Context) ([]shape.LaneTree, error) {
		return []shape.LaneTree{{Dev: "eth0"}}, nil
	}

	fc := v.verify(context.Background(), liveScope).Filters

	assert.Equal(t, CheckOK, fc.Status)
	assert.Contains(t, fc.Detail, "pod may be restarting")
}

func TestVerify_NothingShapedIsNotChecked(t *testing.T) {
	v := fakeVerifier()
	v.laneTrees = func(context.Context) ([]shape.LaneTree, error) { return nil, nil }
	assert.Equal(t, CheckNotChecked, v.verify(context.Background(), liveScope).Filters.Status)

	v.laneTrees = func(context.Context) ([]shape.LaneTree, error) { return nil, errors.New("tc died") }
	assert.Equal(t, CheckUnknown, v.verify(context.Background(), liveScope).Filters.Status)
}

func TestVerify_DeviceStatsErrorIsKeptPerDevice(t *testing.T) {
	v := fakeVerifier()
	v.ifIndex = func(dev string) (int, error) {
		if dev == "lxc1" {
			return 0, errors.New("no such device")
		}
		return 2, nil
	}

	s := v.verify(context.Background(), liveScope).Counters

	assert.Empty(t, s.Devices[0].Error)
	assert.Contains(t, s.Devices[1].Error, "no such device")
}

func TestCapLines(t *testing.T) {
	lines := make([]string, maxDiffLines+5)
	got := capLines(lines)
	assert.Len(t, got, maxDiffLines+1)
	assert.Equal(t, "... and 5 more", got[maxDiffLines])
	assert.Len(t, capLines(lines[:3]), 3)
}
