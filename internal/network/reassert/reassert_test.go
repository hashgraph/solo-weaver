// SPDX-License-Identifier: Apache-2.0

package reassert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeKernel is a scriptable stand-in for the live kernel and the two systemd
// units. Presence is mutable, so a restart can flip an artifact to present.
type fakeKernel struct {
	firewallPresent bool
	policyPresent   bool
	qdiscPresent    bool

	firewallErr error
	policyErr   error
	qdiscErr    error
	// qdiscErrAfterRestart makes the qdisc probe fail once the shaper has been
	// restarted, modelling a re-probe that cannot answer.
	qdiscErrAfterRestart error

	// nftRestarts/shaperRestarts count repair invocations, so coalescing is
	// observable.
	nftRestarts    int
	shaperRestarts int

	nftRestartErr    error
	shaperRestartErr error

	// restartFixes controls whether a successful restart actually restores the
	// artifacts, modelling a repair that runs but does not work.
	restartFixes bool
	// restartHangs makes the nft restart block until its context is cancelled.
	restartHangs bool

	// qdiscDevs records the device each probe was run against.
	qdiscDevs []string

	// lockHeld makes a path report contention (held elsewhere); lockErr makes the
	// acquisition itself fail.
	lockHeld map[string]bool
	lockErr  error
	// lockReleases counts release calls, so leak-free unlocking is observable.
	lockReleases int
	// locksTaken records every path an acquisition succeeded on, in order.
	locksTaken []string
}

// provisioned describes which artifacts exist on disk for a test.
type provisioned struct {
	hostNft   bool
	policyNft bool
	script    bool
	// nic is returned by the egress-NIC reader; scriptErr overrides it.
	nic       string
	scriptErr error
	// shapingDisabled models the teardown render: the script pins nic but builds
	// no hierarchy.
	shapingDisabled bool
	// noNftBinary models a host where no nft binary could be resolved.
	noNftBinary bool
}

// hangingKernel models a wedged systemd restart job: the nft restart blocks until
// its context is cancelled.
func hangingKernel() *fakeKernel { return &fakeKernel{restartHangs: true} }

const (
	testHostNftPath   = "/etc/test/host.nft"
	testPolicyNftPath = "/etc/test/policy.nft"
	testNftLockPath   = "/run/test/.applying"
	testShapeLockPath = "/run/test/.tc-applying"
)

func newTestReasserter(k *fakeKernel, p provisioned) *Reasserter {
	return NewWithConfig(testConfig(k, p))
}

// testConfig is the fixture behind newTestReasserter, exposed so a test can
// override one field before construction.
func testConfig(k *fakeKernel, p provisioned) Config {
	if p.nic == "" {
		p.nic = "eth0"
	}
	return Config{
		HostNftPath:   testHostNftPath,
		PolicyNftPath: testPolicyNftPath,
		NftLockPath:   testNftLockPath,
		ShapeLockPath: testShapeLockPath,
		// Never touch a real /run: the default seam would try to create the lock
		// directory, which is neither present nor creatable on a dev machine.
		AcquireLock: func(path string) (func(), bool, error) {
			if k.lockErr != nil {
				return nil, false, k.lockErr
			}
			if k.lockHeld[path] {
				return nil, false, nil
			}
			k.locksTaken = append(k.locksTaken, path)
			return func() { k.lockReleases++ }, true, nil
		},
		FileExists: func(path string) bool {
			switch path {
			case testHostNftPath:
				return p.hostNft
			case testPolicyNftPath:
				return p.policyNft
			}
			return false
		},
		NftBinary: func() (string, bool) {
			return "/usr/sbin/nft", !p.noNftBinary
		},
		FirewallExists: func(context.Context) (bool, error) {
			return k.firewallPresent, k.firewallErr
		},
		PolicyExists: func(context.Context) (bool, error) {
			return k.policyPresent, k.policyErr
		},
		EgressNIC: func() (string, bool, error) {
			if p.scriptErr != nil {
				return "", false, p.scriptErr
			}
			if !p.script {
				return "", false, nil
			}
			return p.nic, !p.shapingDisabled, nil
		},
		QdiscExists: func(_ context.Context, dev string) (bool, error) {
			k.qdiscDevs = append(k.qdiscDevs, dev)
			return k.qdiscPresent, k.qdiscErr
		},
		RestartNft: func(ctx context.Context) error {
			k.nftRestarts++
			if k.restartHangs {
				<-ctx.Done()
				return ctx.Err()
			}
			// The fix lands before the error is returned, so setting both models
			// a unit that errored while the tables came back anyway.
			if k.restartFixes {
				k.firewallPresent = true
				k.policyPresent = true
			}
			return k.nftRestartErr
		},
		RestartShaper: func(context.Context) error {
			k.shaperRestarts++
			if k.qdiscErrAfterRestart != nil {
				k.qdiscErr = k.qdiscErrAfterRestart
			}
			if k.shaperRestartErr != nil {
				return k.shaperRestartErr
			}
			if k.restartFixes {
				k.qdiscPresent = true
			}
			return nil
		},
	}
}

// artifact pulls one artifact's status out of a report.
func artifact(t *testing.T, r Report, id string) ArtifactStatus {
	t.Helper()
	for _, a := range r.Artifacts {
		if a.Artifact == id {
			return a
		}
	}
	t.Fatalf("artifact %q not present in report", id)
	return ArtifactStatus{}
}

// allProvisioned is the common case: every plane installed on this host.
func allProvisioned() provisioned {
	return provisioned{hostNft: true, policyNft: true, script: true, nic: "eth0"}
}

func TestReport_AlwaysCarriesAllThreeArtifacts(t *testing.T) {
	// Even on a host with nothing provisioned, an operator reading --check must
	// see each plane reported absent rather than missing from the list.
	r := newTestReasserter(&fakeKernel{}, provisioned{}).Check(context.Background())

	require.Len(t, r.Artifacts, 3)
	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy, ArtifactEgressQdisc} {
		a := artifact(t, r, id)
		assert.False(t, a.Expected, "%s should not be expected on an unprovisioned host", id)
		assert.Contains(t, a.Detail, "not provisioned")
	}
}

func TestReassert_UnprovisionedHostRepairsNothing(t *testing.T) {
	k := &fakeKernel{}
	r := newTestReasserter(k, provisioned{}).Reassert(context.Background())

	assert.Zero(t, k.nftRestarts)
	assert.Zero(t, k.shaperRestarts)
	assert.Empty(t, r.Reasserted())
	assert.Empty(t, r.Unhealthy())
}

func TestReassert_AllPresentRepairsNothing(t *testing.T) {
	k := &fakeKernel{firewallPresent: true, policyPresent: true, qdiscPresent: true}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Zero(t, k.nftRestarts)
	assert.Zero(t, k.shaperRestarts)
	assert.Empty(t, r.Reasserted())
	assert.Empty(t, r.Unhealthy())
	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy, ArtifactEgressQdisc} {
		a := artifact(t, r, id)
		assert.True(t, a.Expected)
		assert.True(t, a.Present)
	}
}

func TestReassert_BothNftTablesMissingRestartsSharedUnitOnce(t *testing.T) {
	// The two tables are replayed by one loader unit, so a flush that takes both
	// must cost exactly one restart — not one per table.
	k := &fakeKernel{qdiscPresent: true, restartFixes: true}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Equal(t, 1, k.nftRestarts, "the shared nft loader must be restarted exactly once")
	assert.Zero(t, k.shaperRestarts, "a healthy qdisc must not be rebuilt")

	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy} {
		a := artifact(t, r, id)
		assert.True(t, a.Reasserted, "%s should have been re-asserted", id)
		assert.True(t, a.Recovered, "%s should have recovered", id)
	}
	assert.Empty(t, r.Unhealthy())
}

func TestReassert_OnlyHostFirewallMissingStillRestartsLoaderOnce(t *testing.T) {
	k := &fakeKernel{policyPresent: true, qdiscPresent: true, restartFixes: true}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Equal(t, 1, k.nftRestarts)
	assert.True(t, artifact(t, r, ArtifactHostFirewall).Reasserted)
	// The healthy table is replayed as a side effect of the shared unit, but it
	// was not itself the reason for the repair and must not be reported as one.
	assert.False(t, artifact(t, r, ArtifactWorkloadPolicy).Reasserted)
}

func TestReassert_MissingQdiscRestartsShaperOnly(t *testing.T) {
	k := &fakeKernel{firewallPresent: true, policyPresent: true, restartFixes: true}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Zero(t, k.nftRestarts)
	assert.Equal(t, 1, k.shaperRestarts)

	a := artifact(t, r, ArtifactEgressQdisc)
	assert.True(t, a.Reasserted)
	assert.True(t, a.Recovered)
	assert.Contains(t, a.Detail, "nic=eth0")
}

func TestReassert_ProbesTheNicRecordedInTheBootScript(t *testing.T) {
	// Not the live default route: an operator who pinned --egress-interface must
	// have that device probed, or the check silently watches the wrong one.
	k := &fakeKernel{firewallPresent: true, policyPresent: true, qdiscPresent: true}
	p := allProvisioned()
	p.nic = "bond0"
	newTestReasserter(k, p).Check(context.Background())

	require.NotEmpty(t, k.qdiscDevs)
	assert.Equal(t, "bond0", k.qdiscDevs[0])
}

func TestReassert_DisabledShapingIsNotProvisionedAndNeverRepaired(t *testing.T) {
	// `block node reconfigure` with shaping off leaves a teardown render that pins
	// a NIC but builds no hierarchy; reading that as absent would loop UNRECOVERED.
	k := &fakeKernel{firewallPresent: true, policyPresent: true, qdiscPresent: false}
	p := allProvisioned()
	p.shapingDisabled = true
	r := newTestReasserter(k, p).Reassert(context.Background())

	a := artifact(t, r, ArtifactEgressQdisc)
	assert.False(t, a.Expected)
	assert.False(t, a.Reasserted)
	assert.Contains(t, a.Detail, "egress shaping is disabled")
	assert.Contains(t, a.Detail, "nic=eth0")
	assert.Equal(t, 0, k.shaperRestarts, "a disabled plane must never be repaired")
	assert.Empty(t, k.qdiscDevs, "a disabled plane must not be probed")
	assert.Empty(t, r.Unhealthy())
	assert.Empty(t, r.Missing())
}

func TestReassert_MissingNftBinaryIsProbeFailureNotAbsence(t *testing.T) {
	// The nft runners report an unreachable binary as "table absent". Repairing on
	// that would restart the loader forever on a host with no nft installed.
	k := &fakeKernel{qdiscPresent: true}
	p := allProvisioned()
	p.noNftBinary = true

	r := newTestReasserter(k, p).Reassert(context.Background())

	assert.Zero(t, k.nftRestarts, "a missing nft binary must never trigger a repair")
	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy} {
		a := artifact(t, r, id)
		assert.True(t, a.Expected)
		assert.True(t, a.ProbeFailed)
		assert.False(t, a.Reasserted)
		assert.Contains(t, a.Detail, "no nft binary")
	}
	assert.Len(t, r.Unhealthy(), 2)
}

func TestReassert_QdiscProbeErrorIsNotAbsence(t *testing.T) {
	// The expensive case: netplan renamed the egress device, so tc errors every
	// call. The three-way contract is what stops a rebuild once a minute.
	k := &fakeKernel{
		firewallPresent: true,
		policyPresent:   true,
		qdiscErr:        errors.New("Cannot find device \"eth0\""),
	}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Zero(t, k.shaperRestarts, "a failed probe must never trigger a repair")

	a := artifact(t, r, ArtifactEgressQdisc)
	assert.True(t, a.Expected)
	assert.True(t, a.ProbeFailed)
	assert.False(t, a.Reasserted)
	assert.Contains(t, a.Detail, "cannot determine")
}

func TestReassert_NftProbeErrorIsNotAbsence(t *testing.T) {
	k := &fakeKernel{
		policyPresent: true,
		qdiscPresent:  true,
		firewallErr:   errors.New("permission denied"),
	}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Zero(t, k.nftRestarts)
	a := artifact(t, r, ArtifactHostFirewall)
	assert.True(t, a.ProbeFailed)
	assert.False(t, a.Reasserted)
}

func TestReassert_MalformedBootScriptIsProbeFailureNotAbsence(t *testing.T) {
	// A corrupt script means shaping IS provisioned but the device is unknown.
	// Reading that as "nothing to do here" would hide a real outage.
	k := &fakeKernel{firewallPresent: true, policyPresent: true}
	p := allProvisioned()
	p.scriptErr = errors.New("declares no NIC")

	r := newTestReasserter(k, p).Reassert(context.Background())

	assert.Zero(t, k.shaperRestarts)
	a := artifact(t, r, ArtifactEgressQdisc)
	assert.True(t, a.Expected, "a corrupt script still means shaping is provisioned here")
	assert.True(t, a.ProbeFailed)
}

func TestReassert_RepairFailureIsReportedNotSwallowed(t *testing.T) {
	k := &fakeKernel{qdiscPresent: true, nftRestartErr: errors.New("unit is masked")}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Equal(t, 1, k.nftRestarts)
	a := artifact(t, r, ArtifactHostFirewall)
	assert.True(t, a.Reasserted)
	assert.False(t, a.Recovered)
	assert.Contains(t, a.Detail, "unit is masked")
	assert.NotEmpty(t, r.Unhealthy())
}

func TestReassert_RestartThatDoesNotRestoreIsReportedUnrecovered(t *testing.T) {
	// Distinguishes "a third party keeps wiping it" (recovers every time) from
	// "the repair itself is broken" (never recovers) — different operator problems.
	k := &fakeKernel{qdiscPresent: true, restartFixes: false}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Equal(t, 1, k.nftRestarts)
	a := artifact(t, r, ArtifactWorkloadPolicy)
	assert.True(t, a.Reasserted)
	assert.False(t, a.Recovered)
	assert.Contains(t, a.Detail, "still missing")
}

func TestReassert_PostRepairProbeFailureIsUnknownNotUnrecovered(t *testing.T) {
	// The restart ran, then the re-probe could not answer. Reporting that as
	// "still missing" would send the operator after a repair that may have worked.
	k := &fakeKernel{
		firewallPresent: true, policyPresent: true,
		qdiscErrAfterRestart: errors.New(`Cannot find device "eth0"`),
	}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Equal(t, 1, k.shaperRestarts)
	a := artifact(t, r, ArtifactEgressQdisc)
	assert.True(t, a.Reasserted)
	assert.False(t, a.Recovered)
	assert.True(t, a.ProbeFailed, "an unanswered re-probe is unknown, not a failed repair")
	assert.Contains(t, a.Detail, "re-probe failed")
	assert.Contains(t, a.Detail, `Cannot find device "eth0"`)
	assert.NotContains(t, a.Detail, "still missing")
	assert.NotEmpty(t, r.Unhealthy())
}

func TestReassert_PresentReflectsThePostRepairProbe(t *testing.T) {
	// Present is live state; keeping the pre-repair false would read as absent on
	// /status, which has no Recovered field to show alongside it.
	k := &fakeKernel{qdiscPresent: true, restartFixes: true}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	a := artifact(t, r, ArtifactHostFirewall)
	assert.True(t, a.Reasserted)
	assert.True(t, a.Recovered)
	assert.True(t, a.Present, "a restored artifact must not report present=false")
	assert.Empty(t, r.Missing(), "a restored artifact is not outstanding damage")
}

func TestReassert_PresentStaysFalseWhenTheRepairDidNotRestore(t *testing.T) {
	k := &fakeKernel{qdiscPresent: true, restartFixes: false}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	a := artifact(t, r, ArtifactHostFirewall)
	assert.True(t, a.Reasserted)
	assert.False(t, a.Recovered)
	assert.False(t, a.Present, "an unrecovered artifact must not report present=true")
}

func TestReassert_PresentIsTheReProbeEvenWhenTheRestartErrored(t *testing.T) {
	// systemctl returned non-zero but the table is live again. Present reports
	// what is there; Recovered stays false since our repair did not succeed.
	k := &fakeKernel{
		qdiscPresent:  true,
		restartFixes:  true,
		nftRestartErr: errors.New("job for solo-provisioner-network-nft.service failed"),
	}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	a := artifact(t, r, ArtifactHostFirewall)
	assert.True(t, a.Present, "the table is live, so present must say so")
	assert.False(t, a.Recovered, "our repair errored, so it did not recover it")
	assert.Contains(t, a.Detail, "failed")
	assert.NotEmpty(t, r.Unhealthy(), "an errored repair stays unhealthy")
}

func TestReassert_PresentIsUntouchedWhenTheReProbeCannotAnswer(t *testing.T) {
	// Nothing is claimed either way: ProbeFailed is the answer, and Present must
	// not be read as a fresh measurement.
	k := &fakeKernel{
		firewallPresent: true, policyPresent: true,
		qdiscErrAfterRestart: errors.New(`Cannot find device "eth0"`),
	}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	a := artifact(t, r, ArtifactEgressQdisc)
	assert.True(t, a.ProbeFailed)
	assert.False(t, a.Present)
}

func TestReport_IsTaggedForStreamConsumers(t *testing.T) {
	// The daemon picks the report out of a stream that also carries log events.
	k := &fakeKernel{firewallPresent: true, policyPresent: true, qdiscPresent: true}
	assert.Equal(t, ReportType, newTestReasserter(k, allProvisioned()).Check(context.Background()).Type)
	assert.Equal(t, ReportType, newTestReasserter(k, allProvisioned()).Reassert(context.Background()).Type)
	k.qdiscPresent = false
	assert.Equal(t, ReportType, newTestReasserter(k, allProvisioned()).Reassert(context.Background()).Type)
}

func TestCheck_NeverRepairs(t *testing.T) {
	k := &fakeKernel{restartFixes: true}
	r := newTestReasserter(k, allProvisioned()).Check(context.Background())

	assert.Zero(t, k.nftRestarts, "--check must not mutate anything")
	assert.Zero(t, k.shaperRestarts)
	assert.Empty(t, r.Reasserted())

	// It still reports the outage.
	assert.True(t, artifact(t, r, ArtifactHostFirewall).Expected)
	assert.False(t, artifact(t, r, ArtifactHostFirewall).Present)
}

// TestRepairUnitNamesMatchTheServiceConstants pins the coalescing keys to the
// real unit names, so a one-sided rename cannot silently stop repairing.
func TestRepairUnitNamesMatchTheServiceConstants(t *testing.T) {
	k := &fakeKernel{restartFixes: true}
	rs := newTestReasserter(k, allProvisioned())

	probes := rs.probeAll(context.Background(), openPlaneLock(), openPlaneLock())
	units := map[string]bool{}
	for _, p := range probes {
		if p.unit != "" {
			units[p.unit] = true
		}
	}
	assert.True(t, units[policy.NetworkNftService])
	assert.True(t, units[shape.TcEgressService])
}

func TestReport_MissingReportsOutstandingDamageAfterCheck(t *testing.T) {
	// Check does not repair, so a wiped table stays missing and must be reported.
	// Unhealthy() misses it: nothing probed badly and no repair failed.
	k := &fakeKernel{qdiscPresent: true}
	r := newTestReasserter(k, allProvisioned()).Check(context.Background())

	names := make([]string, 0, 2)
	for _, a := range r.Missing() {
		names = append(names, a.Artifact)
	}
	assert.ElementsMatch(t, []string{ArtifactHostFirewall, ArtifactWorkloadPolicy}, names)
}

func TestReport_MissingIsEmptyAfterASuccessfulReassert(t *testing.T) {
	// Reassert repairs everything it finds absent, so outstanding damage moves
	// into Unhealthy() (if the repair failed) or disappears.
	k := &fakeKernel{qdiscPresent: true, restartFixes: true}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Empty(t, r.Missing())
	assert.Empty(t, r.Unhealthy())
}

func TestReport_MissingExcludesUnprovisionedAndUnprobeable(t *testing.T) {
	k := &fakeKernel{firewallErr: errors.New("boom"), qdiscPresent: true}
	p := provisioned{hostNft: true, policyNft: true} // no boot script
	r := newTestReasserter(k, p).Check(context.Background())

	for _, a := range r.Missing() {
		assert.Equal(t, ArtifactWorkloadPolicy, a.Artifact,
			"only a definitely-absent provisioned artifact counts as missing")
	}
}

// TestReassertSkipsOnlyThePlaneWhoseLockIsHeld pins the apply-lock contract: a
// plane mid-apply is left alone while the other still recovers normally.
func TestReassertSkipsOnlyThePlaneWhoseLockIsHeld(t *testing.T) {
	k := &fakeKernel{
		restartFixes: true,
		lockHeld:     map[string]bool{testNftLockPath: true},
	}
	rs := newTestReasserter(k, allProvisioned())

	r := rs.Reassert(context.Background())

	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy} {
		a := artifact(t, r, id)
		assert.True(t, a.Skipped, "%s must be skipped while the nft apply lock is held", id)
		assert.False(t, a.Reasserted)
		assert.Contains(t, a.Detail, testNftLockPath)
	}
	assert.Zero(t, k.nftRestarts, "the loader must not be restarted underneath an operator apply")

	// The shaper plane has its own lock and is unaffected.
	assert.True(t, artifact(t, r, ArtifactEgressQdisc).Reasserted)
	assert.Equal(t, 1, k.shaperRestarts)
}

// TestSkippedArtifactsAreNeitherMissingNorUnhealthy keeps a benign skip out of
// the two operator-facing summaries — it is contention, not damage.
func TestSkippedArtifactsAreNeitherMissingNorUnhealthy(t *testing.T) {
	k := &fakeKernel{
		lockHeld: map[string]bool{testNftLockPath: true, testShapeLockPath: true},
	}
	rs := newTestReasserter(k, allProvisioned())

	r := rs.Reassert(context.Background())

	require.Len(t, r.Artifacts, 3)
	assert.Empty(t, r.Missing())
	assert.Empty(t, r.Unhealthy())
	assert.Empty(t, r.Reasserted())
	assert.Zero(t, k.nftRestarts)
	assert.Zero(t, k.shaperRestarts)
}

// TestReassertReleasesBothLocks guards against a leaked flock, which would wedge
// every later operator command on the host.
func TestReassertReleasesBothLocks(t *testing.T) {
	k := &fakeKernel{restartFixes: true}
	rs := newTestReasserter(k, allProvisioned())

	rs.Reassert(context.Background())

	assert.Equal(t, []string{testNftLockPath, testShapeLockPath}, k.locksTaken)
	assert.Equal(t, 2, k.lockReleases)
}

// TestReassertReleasesBothLocksWhenNothingNeedsRepair covers the early return a
// healthy host takes, skipping the repair and re-probe entirely.
func TestReassertReleasesBothLocksWhenNothingNeedsRepair(t *testing.T) {
	k := &fakeKernel{firewallPresent: true, policyPresent: true, qdiscPresent: true}
	rs := newTestReasserter(k, allProvisioned())

	r := rs.Reassert(context.Background())

	require.Empty(t, r.Reasserted())
	assert.Equal(t, []string{testNftLockPath, testShapeLockPath}, k.locksTaken)
	assert.Equal(t, 2, k.lockReleases, "the early return must not leak a flock")
}

// TestReassertReportsALockFailureAsUndeterminable separates a broken lock file
// from contention: the former is a fault, and neither may trigger a repair.
func TestReassertReportsALockFailureAsUndeterminable(t *testing.T) {
	k := &fakeKernel{lockErr: errors.New("permission denied")}
	rs := newTestReasserter(k, allProvisioned())

	r := rs.Reassert(context.Background())

	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy, ArtifactEgressQdisc} {
		a := artifact(t, r, id)
		assert.True(t, a.ProbeFailed, "%s", id)
		assert.False(t, a.Skipped, "a lock fault is not benign contention")
		assert.Contains(t, a.Detail, "permission denied")
	}
	assert.Len(t, r.Unhealthy(), 3)
	assert.Zero(t, k.nftRestarts)
	assert.Zero(t, k.shaperRestarts)
}

// TestCheckTakesNoLock keeps a read-only inspection usable while an apply is in
// flight — it mutates nothing, so it has nothing to serialise against.
func TestCheckTakesNoLock(t *testing.T) {
	k := &fakeKernel{
		lockHeld: map[string]bool{testNftLockPath: true, testShapeLockPath: true},
	}
	rs := newTestReasserter(k, allProvisioned())

	r := rs.Check(context.Background())

	assert.Empty(t, k.locksTaken)
	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy, ArtifactEgressQdisc} {
		assert.False(t, artifact(t, r, id).Skipped, "%s must still be probed by --check", id)
	}
	// And it still sees the outage it was asked about.
	assert.Len(t, r.Missing(), 3)
}

// TestReassert_HangingRestartIsCutOffAndReleasesBothLocks is the case the
// deadline exists for: a wedged systemd job must not hold the apply locks hostage.
func TestReassert_HangingRestartIsCutOffAndReleasesBothLocks(t *testing.T) {
	k := hangingKernel()
	k.qdiscPresent = true
	cfg := testConfig(k, allProvisioned())
	cfg.Timeout = 20 * time.Millisecond

	r := NewWithConfig(cfg).Reassert(context.Background())

	assert.Equal(t, 1, k.nftRestarts)
	assert.Equal(t, 2, k.lockReleases, "both flocks must be released after the deadline fires")
	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy} {
		a := artifact(t, r, id)
		assert.True(t, a.Reasserted, "%s", id)
		assert.False(t, a.Recovered, "%s", id)
		assert.Contains(t, a.Detail, "restarting "+policy.NetworkNftService+" failed", "%s", id)
		assert.Contains(t, a.Detail, context.DeadlineExceeded.Error(), "%s", id)
	}
	assert.Len(t, r.Unhealthy(), 2)
	assert.Zero(t, k.shaperRestarts, "the shaper plane was healthy and must stay untouched")
}

// TestReassert_ProbesRunUnderTheDeadline pins that the probes, not only the
// restarts, are bounded: a hung `nft list` holds the locks just the same.
func TestReassert_ProbesRunUnderTheDeadline(t *testing.T) {
	k := &fakeKernel{firewallPresent: true, policyPresent: true, qdiscPresent: true}
	cfg := testConfig(k, allProvisioned())
	var sawDeadline bool
	cfg.FirewallExists = func(ctx context.Context) (bool, error) {
		_, sawDeadline = ctx.Deadline()
		return true, nil
	}

	NewWithConfig(cfg).Reassert(context.Background())

	assert.True(t, sawDeadline, "probe context must carry the run deadline")
}

func TestNewWithConfig_ZeroTimeoutFallsBackToDefault(t *testing.T) {
	assert.Equal(t, DefaultTimeout, newTestReasserter(&fakeKernel{}, allProvisioned()).timeout)
	assert.Positive(t, DefaultTimeout)
}

// TestReassert_EveryArtifactMissingRestartsBothUnitsAndRecoversAll is the full
// blast: a ruleset flush and a netplan apply in the same minute. Both units must
// be restarted exactly once and all three artifacts come back.
func TestReassert_EveryArtifactMissingRestartsBothUnitsAndRecoversAll(t *testing.T) {
	k := &fakeKernel{restartFixes: true}
	r := newTestReasserter(k, allProvisioned()).Reassert(context.Background())

	assert.Equal(t, 1, k.nftRestarts, "the shared nft loader must be restarted exactly once")
	assert.Equal(t, 1, k.shaperRestarts, "the shaper must be restarted exactly once")

	for _, id := range []string{ArtifactHostFirewall, ArtifactWorkloadPolicy, ArtifactEgressQdisc} {
		a := artifact(t, r, id)
		assert.True(t, a.Expected, "%s", id)
		assert.True(t, a.Reasserted, "%s", id)
		assert.True(t, a.Recovered, "%s", id)
		assert.True(t, a.Present, "%s", id)
	}
	assert.Len(t, r.Reasserted(), 3)
	assert.Empty(t, r.Unhealthy())
	assert.Empty(t, r.Missing())
	assert.Empty(t, r.Skipped())
}
