// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/internal/daemon/core"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- test doubles ----------------------------------------------------------

// blockingMonitor implements core.MonitorRunner and blocks until ctx is done.
type blockingMonitor struct{ name string }

func (m *blockingMonitor) Run(ctx context.Context) error { <-ctx.Done(); return nil }
func (m *blockingMonitor) Name() string                  { return m.name }

// connMonitor implements core.ConnectivityMonitor with a fixed error.
type connMonitor struct {
	name string
	cerr *core.StatusError
}

func (m *connMonitor) Run(ctx context.Context) error        { <-ctx.Done(); return nil }
func (m *connMonitor) Name() string                         { return m.name }
func (m *connMonitor) ConnectivityError() *core.StatusError { return m.cerr }

// seqProbe implements core.ComponentProbe returning a scripted sequence of
// errors on successive Probe calls; after the sequence is exhausted it returns
// the final element forever (so a trailing nil means "passes from then on").
type seqProbe struct {
	name string
	errs []error
	// calls is read with atomics because runComponentProbes invokes Probe from a
	// per-component goroutine each round.
	calls int32
}

func (p *seqProbe) ComponentName() string { return p.name }
func (p *seqProbe) Probe(_ context.Context) error {
	i := atomic.AddInt32(&p.calls, 1) - 1
	if int(i) < len(p.errs) {
		return p.errs[i]
	}
	if len(p.errs) == 0 {
		return nil
	}
	return p.errs[len(p.errs)-1]
}

// ---- statusSnapshot --------------------------------------------------------

// seedTracker returns a StatusTracker with name recorded in the "stopped" state
// by driving a no-op monitor through SupervisedMonitor with a cancelled ctx.
func seedTracker(t *testing.T, name string) *core.StatusTracker {
	t.Helper()
	tr := core.NewStatusTracker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	core.SupervisedMonitor(ctx, &blockingMonitor{name: name}, tr)
	require.Equal(t, "stopped", tr.Snapshot()[name].State)
	return tr
}

func TestStatusSnapshot_ConnectivityOverlay_Degrades(t *testing.T) {
	cerr := &core.StatusError{Reason: "WatchFailed", Message: "boom"}
	d := &Daemon{components: []component{{
		name:     "consensus-node",
		monitors: []core.MonitorRunner{&connMonitor{name: "upgrade-monitor", cerr: cerr}},
		tracker:  seedTracker(t, "upgrade-monitor"),
	}}}

	resp := d.statusSnapshot()

	ms := resp.Components["consensus-node"].Monitors["upgrade-monitor"]
	assert.Equal(t, "degraded", ms.State, "connectivity error must overlay state as degraded")
	assert.Same(t, cerr, ms.Error, "connectivity error must be attached verbatim")
}

func TestStatusSnapshot_ConnectivityOverlay_NoErrorPreservesState(t *testing.T) {
	d := &Daemon{components: []component{{
		name:     "consensus-node",
		monitors: []core.MonitorRunner{&connMonitor{name: "upgrade-monitor", cerr: nil}},
		tracker:  seedTracker(t, "upgrade-monitor"),
	}}}

	resp := d.statusSnapshot()

	ms := resp.Components["consensus-node"].Monitors["upgrade-monitor"]
	assert.Equal(t, "stopped", ms.State, "nil connectivity error must not overlay the tracker state")
	assert.Nil(t, ms.Error)
}

func TestStatusSnapshot_NonConnectivityMonitorUntouched(t *testing.T) {
	d := &Daemon{components: []component{{
		name:     "block-node",
		monitors: []core.MonitorRunner{&blockingMonitor{name: "traffic-shaper"}},
		tracker:  seedTracker(t, "traffic-shaper"),
	}}}

	resp := d.statusSnapshot()

	ms := resp.Components["block-node"].Monitors["traffic-shaper"]
	assert.Equal(t, "stopped", ms.State)
	assert.Nil(t, ms.Error)
}

func TestStatusSnapshot_ProbeErrorsOverlay(t *testing.T) {
	d := &Daemon{components: []component{{name: "consensus-node", tracker: core.NewStatusTracker()}}}

	// No probe errors stored → ProbeErrors omitted.
	assert.Empty(t, d.statusSnapshot().ProbeErrors)

	// Stored probe errors → surfaced in the response.
	errs := map[string]core.StatusError{
		"consensus-node": {Reason: "DiskBoom", Message: "no perms", Resolution: "chown"},
	}
	d.probeErrors.Store(&errs)
	got := d.statusSnapshot().ProbeErrors
	require.Len(t, got, 1)
	assert.Equal(t, errs["consensus-node"], got["consensus-node"])

	// An empty (non-nil) map must not surface.
	empty := map[string]core.StatusError{}
	d.probeErrors.Store(&empty)
	assert.Empty(t, d.statusSnapshot().ProbeErrors)
}

// ---- runComponentProbes ----------------------------------------------------

func TestRunComponentProbes_NoProbesReturnsImmediately(t *testing.T) {
	d := &Daemon{components: []component{
		{name: "block-node", probe: nil}, // host-only component, no prerequisites
	}}
	done := make(chan struct{})
	go func() { d.runComponentProbes(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runComponentProbes did not return when there are no probes")
	}
	assert.Nil(t, d.probeErrors.Load(), "probeErrors must be left untouched when no probes exist")
}

func TestRunComponentProbes_OneShotExitOnSuccess(t *testing.T) {
	d := &Daemon{components: []component{
		{name: "consensus-node", probe: &seqProbe{name: "consensus-node"}}, // always passes
	}}
	done := make(chan struct{})
	go func() { d.runComponentProbes(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runComponentProbes did not exit after all probes passed")
	}
	assert.Nil(t, d.probeErrors.Load(), "probeErrors must be cleared to nil when all probes pass")
}

// startProbeLoop runs d.runComponentProbes with a fast retry interval and
// returns a stop func that cancels the loop, waits for it to exit, and only
// then restores the global interval — avoiding a data race on the global
// between the still-running goroutine and the interval restore.
func startProbeLoop(t *testing.T, d *Daemon) (stop func()) {
	t.Helper()
	restore := componentProbeInterval
	componentProbeInterval = 2 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.runComponentProbes(ctx); close(done) }()
	return func() {
		cancel()
		<-done
		componentProbeInterval = restore
	}
}

func TestRunComponentProbes_ExtractsErrorxProperties(t *testing.T) {
	tagged := errorx.ExternalError.New("disk not writable").
		WithProperty(models.ErrPropertyReason, "UpgradeDirOwnership").
		WithProperty(models.ErrPropertyResolution, []string{"chown hedera:hedera /x", "chmod 0700 /x"})

	d := &Daemon{components: []component{
		{name: "consensus-node", probe: &seqProbe{name: "consensus-node", errs: []error{tagged}}},
	}}
	defer startProbeLoop(t, d)()

	require.Eventually(t, func() bool {
		pe := d.probeErrors.Load()
		return pe != nil && len(*pe) == 1
	}, time.Second, 2*time.Millisecond, "probe error was never recorded")

	se := (*d.probeErrors.Load())["consensus-node"]
	assert.Equal(t, "UpgradeDirOwnership", se.Reason, "Reason must come from the errorx property, not the default")
	assert.Equal(t, "chown hedera:hedera /x; chmod 0700 /x", se.Resolution, "Resolution must join the property slice")
	assert.Contains(t, se.Message, "disk not writable")
	assert.NotEmpty(t, se.Since)
}

func TestRunComponentProbes_PlainErrorUsesDefaultReason(t *testing.T) {
	plain := errorx.ExternalError.New("something failed") // no reason/resolution properties
	d := &Daemon{components: []component{
		{name: "consensus-node", probe: &seqProbe{name: "consensus-node", errs: []error{plain}}},
	}}
	defer startProbeLoop(t, d)()

	require.Eventually(t, func() bool {
		pe := d.probeErrors.Load()
		return pe != nil && len(*pe) == 1
	}, time.Second, 2*time.Millisecond)

	se := (*d.probeErrors.Load())["consensus-node"]
	assert.Equal(t, "ComponentProbeError", se.Reason, "plain errors must fall back to the default reason")
	assert.Empty(t, se.Resolution)
}

func TestRunComponentProbes_PreservesSinceAcrossRetries(t *testing.T) {
	always := errorx.ExternalError.New("still failing")
	d := &Daemon{components: []component{
		{name: "consensus-node", probe: &seqProbe{name: "consensus-node", errs: []error{always}}},
	}}
	defer startProbeLoop(t, d)()

	require.Eventually(t, func() bool {
		pe := d.probeErrors.Load()
		return pe != nil && len(*pe) == 1
	}, time.Second, 2*time.Millisecond)
	first := (*d.probeErrors.Load())["consensus-node"].Since

	// Let several more retry rounds elapse.
	time.Sleep(40 * time.Millisecond)
	second := (*d.probeErrors.Load())["consensus-node"].Since
	assert.Equal(t, first, second, "Since must reflect the first failure, not the latest check")
}

// ---- NewFromConfig ---------------------------------------------------------

func TestNewFromConfig_NoComponentsStillBuildsServer(t *testing.T) {
	d, err := NewFromConfig(models.WeaverPaths{DaemonSockPath: "/tmp/x.sock"}, DaemonConfig{})
	require.NoError(t, err)
	assert.Empty(t, d.components)
	assert.NotNil(t, d.server, "server must always be constructed")
}

func TestNewFromConfig_DisabledComponentSkipped(t *testing.T) {
	cfg := DaemonConfig{Components: DaemonComponents{
		ConsensusNode: &ConsensusNodeComponentConfig{Enabled: false},
		BlockNode:     &BlockNodeComponentConfig{Enabled: false},
	}}
	d, err := NewFromConfig(models.WeaverPaths{DaemonSockPath: "/tmp/x.sock"}, cfg)
	require.NoError(t, err)
	assert.Empty(t, d.components)
}

func TestNewFromConfig_BlockNodeOnly(t *testing.T) {
	cfg := DaemonConfig{Components: DaemonComponents{
		BlockNode: &BlockNodeComponentConfig{
			Enabled:  true,
			Monitors: BlockNodeMonitors{TrafficShaper: true},
		},
	}}
	d, err := NewFromConfig(models.WeaverPaths{DaemonSockPath: "/tmp/x.sock"}, cfg)
	require.NoError(t, err)
	require.Len(t, d.components, 1)
	assert.Equal(t, "block-node", d.components[0].name)
	assert.Nil(t, d.components[0].probe, "host-only block-node component must have a nil probe")
	assert.Len(t, d.components[0].monitors, 1)
}

func TestNewFromConfig_BlockNodeEnabledNoMonitorsProducesNoComponent(t *testing.T) {
	cfg := DaemonConfig{Components: DaemonComponents{
		BlockNode: &BlockNodeComponentConfig{Enabled: true}, // TrafficShaper off → zero monitors
	}}
	d, err := NewFromConfig(models.WeaverPaths{DaemonSockPath: "/tmp/x.sock"}, cfg)
	require.NoError(t, err)
	assert.Empty(t, d.components, "a component with no enabled monitors must be dropped")
}

func TestNewFromConfig_ConsensusNodeWiring(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "cn.kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfig, []byte(minimalKubeconfig), 0o600))

	paths := models.WeaverPaths{
		HomeDir:                         dir,
		DaemonSockPath:                  filepath.Join(dir, "d.sock"),
		DaemonConsensusUpgradeEventsDir: filepath.Join(dir, "events", "upgrade"),
		DaemonConsensusMigrateEventsDir: filepath.Join(dir, "events", "migrate"),
	}
	cfg := DaemonConfig{Components: DaemonComponents{
		ConsensusNode: &ConsensusNodeComponentConfig{
			Enabled:    true,
			Kubeconfig: kubeconfig,
			NodeID:     "0",
			Orbit:      "hedera-network",
			Monitors:   ConsensusNodeMonitors{Upgrade: true, Migration: true},
		},
	}}

	d, err := NewFromConfig(paths, cfg)
	require.NoError(t, err)
	require.Len(t, d.components, 1)
	comp := d.components[0]
	assert.Equal(t, "consensus-node", comp.name)
	assert.Len(t, comp.monitors, 2, "upgrade + migration monitors must both be wired")
	assert.NotNil(t, comp.tracker)
	assert.NotNil(t, comp.probe, "the upgrade monitor declares an RBAC prerequisite, so the component probe must be built")
	assert.Equal(t, "consensus-node", comp.probe.ComponentName())
}

const minimalKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: faketoken
`
