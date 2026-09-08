// SPDX-License-Identifier: Apache-2.0

// Package network hosts the daemon's host-network monitors. It is separate from
// the block-node component because the host firewall exists without shaping.
package network

import (
	"context"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/automa-saga/logx"
	"github.com/rs/zerolog"

	"github.com/hashgraph/solo-weaver/internal/daemon/privexec"
)

// MonitorName is the monitor's name as reported by GET /status.
const MonitorName = "network-reassert-monitor"

// reassertInterval is how often the presence check runs: a minute, unlike the
// shaper monitor's hourly resync, since a missing firewall leaves the node
// exposed for the whole interval. A var so tests can shrink it.
var reassertInterval = time.Minute

// reassertTickTimeout bounds one worker exec. It must stay longer than
// reassert.DefaultTimeout so the worker, which holds the locks, gives up first.
var reassertTickTimeout = 3 * time.Minute

// reassertStartupGrace delays the first check: at boot the shaper unit is still
// waiting for network-online and its device. A var so tests can shrink it.
var reassertStartupGrace = 2 * time.Minute

// Artifact paths that decide whether this host has anything to check. Mirrored
// by value so the daemon does not import the exec-backed network packages.
const (
	hostFirewallNftPath   = "/etc/solo-provisioner/network-weaver-host-firewall.nft"
	workloadPolicyNftPath = "/etc/solo-provisioner/network-weaver-workload-policy.nft"
	tcEgressScriptPath    = "/usr/local/sbin/solo-provisioner-bandwidth-shaper.sh"
)

// ArtifactState is one artifact's running history, as reported by GET /status.
type ArtifactState struct {
	Artifact    string `json:"artifact"`
	Expected    bool   `json:"expected"`
	Present     bool   `json:"present"`
	ProbeFailed bool   `json:"probe_failed,omitempty"`
	Detail      string `json:"detail,omitempty"`
	// ReassertCount is how many times this artifact has been restored.
	ReassertCount int `json:"reassert_count"`
	// LastReassertedAt is when it was last restored, nil if never.
	LastReassertedAt *time.Time `json:"last_reasserted_at,omitempty"`
	// ConsecutiveFailures counts successive runs where the repair did not work.
	ConsecutiveFailures int `json:"consecutive_failures,omitempty"`
}

// Snapshot is the component payload served under GET /status.
type Snapshot struct {
	// LastCheckAt is when a run last probed something, nil before the first one.
	LastCheckAt *time.Time `json:"last_check_at,omitempty"`
	// LastError is the most recent worker-exec failure, cleared on the next success.
	LastError string `json:"last_error,omitempty"`
	// Artifacts is nil until the first successful run.
	Artifacts []ArtifactState `json:"artifacts,omitempty"`
}

// ReassertMonitor periodically runs `network reassert` under sudo and keeps the
// history that GET /status reports.
type ReassertMonitor struct {
	delegator privexec.Delegator

	// gatePaths are stat-ed before each tick; none present means nothing to check.
	gatePaths []string
	// fileExists is a seam so tests need no real /etc.
	fileExists func(path string) bool

	mu    sync.Mutex
	state Snapshot
}

// NewReassertMonitor constructs a ReassertMonitor wired to the sudo-backed
// delegator.
func NewReassertMonitor() *ReassertMonitor {
	return &ReassertMonitor{
		delegator: privexec.New(),
		gatePaths: []string{hostFirewallNftPath, workloadPolicyNftPath, tcEgressScriptPath},
		fileExists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
	}
}

// Name implements daemonkit.MonitorRunner.
func (m *ReassertMonitor) Name() string { return MonitorName }

// Snapshot returns a copy of the current history. Safe for concurrent use.
func (m *ReassertMonitor) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := m.state
	if m.state.Artifacts != nil {
		out.Artifacts = make([]ArtifactState, len(m.state.Artifacts))
		copy(out.Artifacts, m.state.Artifacts)
	}
	return out
}

// Run implements daemonkit.MonitorRunner. It checks after reassertStartupGrace,
// then every reassertInterval. A worker-exec failure is returned so the supervisor backs off.
func (m *ReassertMonitor) Run(ctx context.Context) error {
	logx.As().Info().
		Str("reason", "NetworkReassertMonitorStarting").
		Str("monitor", m.Name()).
		Dur("startup_grace", reassertStartupGrace).
		Dur("interval", reassertInterval).
		Msg("network re-assert monitor starting")

	select {
	case <-ctx.Done():
		return nil
	case <-time.After(reassertStartupGrace):
	}

	ticker := time.NewTicker(reassertInterval)
	defer ticker.Stop()

	if err := m.tick(ctx); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := m.tick(ctx); err != nil {
				return err
			}
		}
	}
}

// tick runs one presence check.
func (m *ReassertMonitor) tick(ctx context.Context) error {
	// Re-evaluated every tick, so a host provisioned later is picked up.
	if !m.anyArtifactProvisioned() {
		return nil
	}

	tickCtx, cancel := context.WithTimeout(ctx, reassertTickTimeout)
	defer cancel()

	res, err := m.delegator.NetworkReassert(tickCtx)
	if err != nil {
		// Shutdown is not a fault; a tick that hit its own deadline is.
		if ctx.Err() != nil {
			return nil
		}
		m.recordExecFailure(err)
		logx.As().Error().Err(err).
			Str("reason", "NetworkReassertWorkerFailed").
			Str("monitor", m.Name()).
			Msg("network re-assert worker failed")
		return err
	}

	m.record(res)
	return nil
}

// anyArtifactProvisioned reports whether any weaver network artifact is on disk.
func (m *ReassertMonitor) anyArtifactProvisioned() bool {
	return slices.ContainsFunc(m.gatePaths, m.fileExists)
}

// recordExecFailure stores a worker-exec failure; the artifact history is kept.
func (m *ReassertMonitor) recordExecFailure(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.LastError = err.Error()
}

// record folds one worker report into the history and logs what an operator
// needs to see.
func (m *ReassertMonitor) record(res privexec.NetworkReassertResult) {
	now := time.Now().UTC()

	m.mu.Lock()
	prev := indexByArtifact(m.state.Artifacts)
	next := make([]ArtifactState, 0, len(res.Artifacts))
	probedAny := false
	for _, a := range res.Artifacts {
		// A skipped artifact was not looked at, so its last known state carries over.
		if a.Skipped {
			if p, ok := prev[a.Artifact]; ok {
				next = append(next, p)
				continue
			}
			next = append(next, ArtifactState{Artifact: a.Artifact, Detail: a.Detail})
			continue
		}

		probedAny = true
		st := ArtifactState{
			Artifact:    a.Artifact,
			Expected:    a.Expected,
			Present:     a.Present,
			ProbeFailed: a.ProbeFailed,
			Detail:      a.Detail,
		}
		if p, ok := prev[a.Artifact]; ok {
			st.ReassertCount = p.ReassertCount
			st.LastReassertedAt = p.LastReassertedAt
			st.ConsecutiveFailures = p.ConsecutiveFailures
		}

		switch {
		case a.Reasserted && a.Recovered:
			st.ReassertCount++
			at := now
			st.LastReassertedAt = &at
			st.ConsecutiveFailures = 0
		case a.ProbeFailed || (a.Reasserted && !a.Recovered):
			st.ConsecutiveFailures++
		default:
			st.ConsecutiveFailures = 0
		}
		next = append(next, st)
	}
	m.state.Artifacts = next
	// An all-skipped run saw nothing, so it does not count as a check.
	if probedAny {
		m.state.LastCheckAt = &now
	}
	m.state.LastError = ""
	m.mu.Unlock()

	m.logReport(res, next, prev)
}

// logReport emits one line per artifact that needs attention: WARN for a
// restore, and ERROR only on the first tick of a failure (DEBUG after, since
// GET /status already tracks the count).
func (m *ReassertMonitor) logReport(
	res privexec.NetworkReassertResult,
	states []ArtifactState,
	prev map[string]ArtifactState,
) {
	counts := indexByArtifact(states)
	for _, a := range res.Artifacts {
		st := counts[a.Artifact]
		switch {
		case a.Skipped:
			// Nothing was observed; the skip reason is in Detail for GET /status.
		case a.Reasserted && a.Recovered:
			logx.As().Warn().
				Str("reason", "NetworkArtifactReasserted").
				Str("monitor", m.Name()).
				Str("artifact", a.Artifact).
				Int("reassert_count", st.ReassertCount).
				Str("detail", a.Detail).
				Msg("weaver network state was missing and has been restored")
		// Checked before Reasserted: a repair whose re-probe failed is unknown, not
		// unrecovered.
		case a.ProbeFailed:
			logx.As().WithLevel(failureLevel(st.ConsecutiveFailures)).
				Str("reason", "NetworkArtifactProbeFailed").
				Str("monitor", m.Name()).
				Str("artifact", a.Artifact).
				Int("consecutive_failures", st.ConsecutiveFailures).
				Str("detail", a.Detail).
				Msg("could not determine whether weaver network state is present")
		case a.Reasserted:
			logx.As().WithLevel(failureLevel(st.ConsecutiveFailures)).
				Str("reason", "NetworkArtifactUnrecovered").
				Str("monitor", m.Name()).
				Str("artifact", a.Artifact).
				Int("consecutive_failures", st.ConsecutiveFailures).
				Str("detail", a.Detail).
				Msg("weaver network state is missing and the repair did not restore it")
		case prev[a.Artifact].ConsecutiveFailures > 0:
			logx.As().Info().
				Str("reason", "NetworkArtifactHealthyAgain").
				Str("monitor", m.Name()).
				Str("artifact", a.Artifact).
				Int("failed_runs", prev[a.Artifact].ConsecutiveFailures).
				Msg("weaver network state is present again")
		}
	}
}

// failureLevel is ERROR for a new failure and DEBUG for every repeat.
func failureLevel(consecutive int) zerolog.Level {
	if consecutive <= 1 {
		return zerolog.ErrorLevel
	}
	return zerolog.DebugLevel
}

// indexByArtifact keys states by artifact name.
func indexByArtifact(states []ArtifactState) map[string]ArtifactState {
	out := make(map[string]ArtifactState, len(states))
	for _, s := range states {
		out[s.Artifact] = s
	}
	return out
}
