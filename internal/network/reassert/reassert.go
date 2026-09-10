// SPDX-License-Identifier: Apache-2.0

// Package reassert restores weaver's nft tables and the $EGRESS HTB hierarchy
// after a third party removes them. See docs/commands/network/reassert.md.
package reassert

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/nftexec"
	"github.com/hashgraph/solo-weaver/internal/network/policy"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
)

// DefaultTimeout bounds one Reassert run, which holds both apply locks throughout.
const DefaultTimeout = 2 * time.Minute

// Artifact identifiers. Part of the JSON contract with the daemon.
const (
	ArtifactHostFirewall   = "host-firewall"
	ArtifactWorkloadPolicy = "workload-policy"
	ArtifactEgressQdisc    = "egress-qdisc"
)

// ArtifactStatus is one artifact's outcome for a single run.
type ArtifactStatus struct {
	Artifact string `json:"artifact"`
	// Expected is true when the artifact is provisioned on this host.
	Expected bool `json:"expected"`
	// Present is true when the artifact is live in the kernel — after a repair
	// that is the re-probe's answer, not the state the run found.
	Present bool `json:"present"`
	// ProbeFailed means presence could not be determined: before a repair, so
	// nothing is repaired, or on the re-probe after one, so Recovered is unknown.
	ProbeFailed bool `json:"probe_failed,omitempty"`
	// Skipped means an operator apply held the lock, so this run left it alone.
	Skipped bool `json:"skipped,omitempty"`
	// Reasserted means a repair was attempted on this run.
	Reasserted bool `json:"reasserted,omitempty"`
	// Recovered means the re-probe after the repair found the artifact present.
	Recovered bool `json:"recovered,omitempty"`
	// Detail is the NIC, the probe error, or the skip reason.
	Detail string `json:"detail,omitempty"`
}

// ReportType tags the JSON report so a consumer selects it by tag rather than
// by position (see privexec.ParseNetworkReassertReport).
const ReportType = "reassert"

// Report is the result of one Check or Reassert. It always lists all three
// artifacts in a stable order.
type Report struct {
	// Type is always ReportType.
	Type      string           `json:"type"`
	Artifacts []ArtifactStatus `json:"artifacts"`
}

// Reasserted returns the artifacts a repair was attempted for.
func (r Report) Reasserted() []ArtifactStatus {
	return r.filter(func(a ArtifactStatus) bool { return a.Reasserted })
}

// Unhealthy returns the artifacts that could not be probed or did not recover.
func (r Report) Unhealthy() []ArtifactStatus {
	return r.filter(func(a ArtifactStatus) bool {
		return a.ProbeFailed || (a.Reasserted && !a.Recovered)
	})
}

// Skipped returns the artifacts this run did not look at because a lock was held.
func (r Report) Skipped() []ArtifactStatus {
	return r.filter(func(a ArtifactStatus) bool { return a.Skipped })
}

// Missing returns the artifacts that are expected, absent, and not repaired.
// Always empty after Reassert.
func (r Report) Missing() []ArtifactStatus {
	return r.filter(func(a ArtifactStatus) bool {
		return a.Expected && !a.Present && !a.ProbeFailed && !a.Reasserted
	})
}

func (r Report) filter(keep func(ArtifactStatus) bool) []ArtifactStatus {
	var out []ArtifactStatus
	for _, a := range r.Artifacts {
		if keep(a) {
			out = append(out, a)
		}
	}
	return out
}

// Reasserter probes the three artifacts and restores the ones that are
// definitely missing. Every dependency is a seam so tests need no kernel.
type Reasserter struct {
	hostNftPath   string
	policyNftPath string
	nftLockPath   string
	shapeLockPath string
	timeout       time.Duration

	fileExists     func(path string) bool
	acquireLock    func(path string) (release func(), acquired bool, err error)
	nftBinary      func() (string, bool)
	firewallExists func(ctx context.Context) (bool, error)
	policyExists   func(ctx context.Context) (bool, error)
	egressNIC      func() (string, bool, error)
	qdiscExists    func(ctx context.Context, nic string) (bool, error)
	restartNft     func(ctx context.Context) error
	restartShaper  func(ctx context.Context) error
}

// Config customises a Reasserter. Unset fields take their production defaults.
type Config struct {
	HostNftPath   string
	PolicyNftPath string
	NftLockPath   string
	ShapeLockPath string
	// Timeout bounds one Reassert run; zero means DefaultTimeout.
	Timeout time.Duration
	// AcquireLock takes one plane's apply lock without blocking. Its release func
	// must be safe to call even when the lock was not taken.
	AcquireLock    func(path string) (release func(), acquired bool, err error)
	FileExists     func(path string) bool
	NftBinary      func() (string, bool)
	FirewallExists func(ctx context.Context) (bool, error)
	PolicyExists   func(ctx context.Context) (bool, error)
	EgressNIC      func() (string, bool, error)
	QdiscExists    func(ctx context.Context, nic string) (bool, error)
	RestartNft     func(ctx context.Context) error
	RestartShaper  func(ctx context.Context) error
}

// New returns a Reasserter wired to the live kernel and production paths.
func New() *Reasserter { return NewWithConfig(Config{}) }

// NewWithConfig returns a Reasserter, filling unset Config fields with defaults.
func NewWithConfig(cfg Config) *Reasserter {
	r := &Reasserter{
		hostNftPath:    cfg.HostNftPath,
		policyNftPath:  cfg.PolicyNftPath,
		nftLockPath:    cfg.NftLockPath,
		shapeLockPath:  cfg.ShapeLockPath,
		timeout:        cfg.Timeout,
		acquireLock:    cfg.AcquireLock,
		fileExists:     cfg.FileExists,
		nftBinary:      cfg.NftBinary,
		firewallExists: cfg.FirewallExists,
		policyExists:   cfg.PolicyExists,
		egressNIC:      cfg.EgressNIC,
		qdiscExists:    cfg.QdiscExists,
		restartNft:     cfg.RestartNft,
		restartShaper:  cfg.RestartShaper,
	}
	if r.hostNftPath == "" {
		r.hostNftPath = firewall.HostNftPath
	}
	if r.policyNftPath == "" {
		r.policyNftPath = policy.WeaverNftPath
	}
	// The firewall and policy managers share one nft lock; the shaper has its own.
	if r.nftLockPath == "" {
		r.nftLockPath = policy.LockPath
	}
	if r.shapeLockPath == "" {
		r.shapeLockPath = shape.ShapeLockPath
	}
	if r.timeout <= 0 {
		r.timeout = DefaultTimeout
	}
	if r.acquireLock == nil {
		r.acquireLock = flockNB
	}
	if r.fileExists == nil {
		r.fileExists = func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		}
	}
	if r.nftBinary == nil {
		r.nftBinary = nftexec.Binary
	}
	if r.firewallExists == nil {
		r.firewallExists = firewall.NewExecRunner().Exists
	}
	if r.policyExists == nil {
		r.policyExists = policy.NewExecRunner().Exists
	}
	if r.egressNIC == nil {
		r.egressNIC = shape.EgressNICFromScript
	}
	if r.qdiscExists == nil {
		r.qdiscExists = shape.QdiscRootExists
	}
	if r.restartNft == nil {
		r.restartNft = policy.RestartNetworkNftService
	}
	if r.restartShaper == nil {
		r.restartShaper = shape.RestartTcEgressService
	}
	return r
}

// probeResult is an artifact's status plus the unit to restart, if any.
type probeResult struct {
	status ArtifactStatus
	unit   string
}

// Check probes all three artifacts without locking or changing anything.
func (r *Reasserter) Check(ctx context.Context) Report {
	return reportOf(r.probeAll(ctx, openPlaneLock(), openPlaneLock()))
}

// Reassert probes all three artifacts, restarts each unit with a missing
// artifact once, then re-probes. Both plane locks are held for the whole run.
func (r *Reasserter) Reassert(ctx context.Context) Report {
	// The deadline lives here because this process holds the locks.
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	nftLock := r.lockPlane(r.nftLockPath)
	defer nftLock.release()
	shapeLock := r.lockPlane(r.shapeLockPath)
	defer shapeLock.release()

	before := r.probeAll(ctx, nftLock, shapeLock)

	// At most one restart per unit, in probe order.
	repaired := make(map[string]error, 2)
	for _, p := range before {
		if p.unit == "" {
			continue
		}
		if _, done := repaired[p.unit]; done {
			continue
		}
		repaired[p.unit] = r.repair(ctx, p.unit)
	}
	if len(repaired) == 0 {
		return reportOf(before)
	}

	// probeAll has a fixed order, so index i is the same artifact in both passes.
	after := r.probeAll(ctx, nftLock, shapeLock)
	out := make([]ArtifactStatus, len(before))
	for i, p := range before {
		st := p.status
		if p.unit != "" {
			st.Reasserted = true
			// Present is live state, so it comes from the re-probe wherever
			// that answered — the pre-repair false would outlive the repair.
			if !after[i].status.ProbeFailed {
				st.Present = after[i].status.Present
			}
			if err := repaired[p.unit]; err != nil {
				st.Detail = withDetail(st.Detail, "restarting "+p.unit+" failed: "+err.Error())
			} else if after[i].status.ProbeFailed {
				// Unknown, not "still missing": the re-probe could not answer.
				st.ProbeFailed = true
				st.Detail = withDetail(st.Detail,
					"restarted "+p.unit+" but the re-probe failed: "+probeCause(after[i].status))
			} else {
				st.Recovered = st.Present
				if !st.Recovered {
					st.Detail = withDetail(st.Detail,
						"restarted "+p.unit+" but the artifact is still missing")
				}
			}
		}
		out[i] = st
	}
	return Report{Type: ReportType, Artifacts: out}
}

// repair restarts the named unit.
func (r *Reasserter) repair(ctx context.Context, unit string) error {
	switch unit {
	case policy.NetworkNftService:
		return r.restartNft(ctx)
	case shape.TcEgressService:
		return r.restartShaper(ctx)
	default:
		// Unreachable: probeAll only assigns the two units above.
		return nil
	}
}

// probeAll returns the three artifacts' state in a stable order.
func (r *Reasserter) probeAll(ctx context.Context, nftLock, shapeLock planeLock) []probeResult {
	return []probeResult{
		r.probeNftTable(ctx, ArtifactHostFirewall, r.hostNftPath, r.firewallExists, nftLock),
		r.probeNftTable(ctx, ArtifactWorkloadPolicy, r.policyNftPath, r.policyExists, nftLock),
		r.probeEgressQdisc(ctx, shapeLock),
	}
}

// probeNftTable resolves one nft table's state. The persisted .nft file decides
// whether the table is expected here.
func (r *Reasserter) probeNftTable(
	ctx context.Context,
	id, artifactPath string,
	exists func(context.Context) (bool, error),
	lock planeLock,
) probeResult {
	if st, skip := lock.skip(id); skip {
		return probeResult{status: st}
	}

	st := ArtifactStatus{Artifact: id}

	if !r.fileExists(artifactPath) {
		st.Detail = "not provisioned on this host (" + artifactPath + " absent)"
		return probeResult{status: st}
	}
	st.Expected = true

	// Checked first so a missing binary gets a clear message, not a bare exec error.
	if bin, ok := r.nftBinary(); !ok {
		st.ProbeFailed = true
		st.Detail = "cannot determine: no nft binary found (looked for " + bin + ")"
		return probeResult{status: st}
	}

	present, err := exists(ctx)
	if err != nil {
		st.ProbeFailed = true
		st.Detail = "cannot determine: " + err.Error()
		return probeResult{status: st}
	}
	st.Present = present
	if present {
		return probeResult{status: st}
	}
	return probeResult{status: st, unit: policy.NetworkNftService}
}

// probeEgressQdisc resolves the $EGRESS HTB root qdisc's state. The boot script
// decides whether shaping is expected here and names the NIC.
func (r *Reasserter) probeEgressQdisc(ctx context.Context, lock planeLock) probeResult {
	if st, skip := lock.skip(ArtifactEgressQdisc); skip {
		return probeResult{status: st}
	}

	st := ArtifactStatus{Artifact: ArtifactEgressQdisc}

	nic, provisioned, err := r.egressNIC()
	if err != nil {
		// The script exists but cannot be read: a probe failure, not an absence.
		st.Expected = true
		st.ProbeFailed = true
		st.Detail = "cannot determine: " + err.Error()
		return probeResult{status: st}
	}
	if !provisioned {
		// A NIC with no hierarchy is the teardown render: shaping is disabled.
		if nic != "" {
			st.Detail = "not provisioned on this host (egress shaping is disabled: " +
				shape.TcEgressScriptPath + " is the teardown render for nic=" + nic + ")"
		} else {
			st.Detail = "not provisioned on this host (" + shape.TcEgressScriptPath + " absent)"
		}
		return probeResult{status: st}
	}
	st.Expected = true
	st.Detail = "nic=" + nic

	present, err := r.qdiscExists(ctx, nic)
	if err != nil {
		st.ProbeFailed = true
		st.Detail = withDetail(st.Detail, "cannot determine: "+err.Error())
		return probeResult{status: st}
	}
	st.Present = present
	if present {
		return probeResult{status: st}
	}
	return probeResult{status: st, unit: shape.TcEgressService}
}

// reportOf projects probe results into a Report.
func reportOf(probes []probeResult) Report {
	out := make([]ArtifactStatus, len(probes))
	for i, p := range probes {
		out[i] = p.status
	}
	return Report{Type: ReportType, Artifacts: out}
}

// probeCause returns the cause behind a failed probe's "cannot determine" clause.
func probeCause(st ArtifactStatus) string {
	const marker = "cannot determine: "
	if i := strings.LastIndex(st.Detail, marker); i >= 0 {
		return st.Detail[i+len(marker):]
	}
	return st.Detail
}

// withDetail appends a clause to an existing detail string.
func withDetail(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "; " + add
}
