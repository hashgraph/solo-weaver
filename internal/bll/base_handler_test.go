// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package bll

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/internal/rsl"
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
	"github.com/joomcode/errorx"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/release"
)

type funcChecker[T any] func() (T, error)

func (f funcChecker[T]) RefreshState(context.Context) (T, error) { return f() }

// host stands in for the live system: the checkers read teleport from it, and
// the workflow can change it as a real install would. A non-nil teleportProbe
// swaps in the real Teleport checker with that cluster probe and no Helm releases.
// A non-nil software replaces the recorded host binaries, as the machine
// checker's probe would after an install.
type host struct {
	teleport      state.TeleportState
	teleportProbe reality.ClusterProbe
	software      map[string]state.SoftwareState
}

type noReleases struct{}

func (noReleases) ListAll() ([]*release.Release, error) { return nil, nil }

// noopIntent is a converging test handler. It runs a one-step workflow that
// calls execute. With buildErr set, BuildWorkflow fails; with noSteps set, it
// returns a workflow with no steps, which then fails to build; with stepErr
// set, the step fails.
type noopIntent struct {
	execute  func()
	buildErr error
	noSteps  bool
	stepErr  error
	// recordVersion adds the real RecordProvisionerVersion step after the noop.
	recordVersion bool
}

func (n noopIntent) PrepareEffectiveInputs(_ models.Intent, in models.UserInputs[struct{}]) (*models.UserInputs[struct{}], error) {
	return &in, nil
}

func (n noopIntent) BuildWorkflow(state.State, models.UserInputs[struct{}]) (*automa.WorkflowBuilder, error) {
	if n.buildErr != nil {
		return nil, n.buildErr
	}
	if n.noSteps {
		return automa.NewWorkflowBuilder().WithId("test"), nil
	}
	step := automa.NewStepBuilder().WithId("noop").
		WithExecute(func(_ context.Context, stp automa.Step) *automa.Report {
			if n.execute != nil {
				n.execute()
			}
			if n.stepErr != nil {
				return automa.FailureReport(stp, automa.WithError(n.stepErr))
			}
			return automa.SuccessReport(stp)
		})
	if n.recordVersion {
		return automa.NewWorkflowBuilder().WithId("test").Steps(step, steps.RecordProvisionerVersion()), nil
	}
	return automa.NewWorkflowBuilder().WithId("test").Steps(step), nil
}

func (n noopIntent) HandleIntent(context.Context, models.Intent, models.UserInputs[struct{}]) (*automa.Report, error) {
	return nil, nil
}

// handlerFileManager wraps the file manager handleIntent gives the command's
// state manager, so a test can make its appends fail.
var handlerFileManager = func(fm fsx.Manager) fsx.Manager { return fm }

// unsyncedAppender is an fsx.Manager whose appends land but report a failed sync.
type unsyncedAppender struct{ fsx.Manager }

func (a unsyncedAppender) AppendToFile(path string, payload []byte) error {
	if err := a.Manager.AppendToFile(path, payload); err != nil {
		return err
	}
	return fsx.FileSyncError.New("disk gone")
}

func newFileManager(t *testing.T) fsx.Manager {
	t.Helper()
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(principal.NewMockManager(gomock.NewController(t))))
	require.NoError(t, err)
	return fm
}

func writeState(t *testing.T, stateFile string, teleport state.TeleportState) {
	t.Helper()
	s := state.NewState(stateFile)
	s.TeleportState = teleport
	sm, err := state.NewStateManager(state.WithState(s), state.WithFileManager(newFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.Commit(state.ActionHistory{}, state.AllComponentIDs...))
	// The seed is not an action under test: drop its entry.
	require.NoError(t, os.Remove(state.ActionHistoryFilePath(filepath.Dir(stateFile))))
}

func readState(t *testing.T, stateFile string) state.State {
	t.Helper()
	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(newFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())
	return sm.State()
}

// runBlockNodeIntent runs one successful block node install through BaseHandler.
func runBlockNodeIntent(t *testing.T, stateFile string, h *host, intent noopIntent) *automa.Report {
	t.Helper()
	report, err := handleBlockNodeIntent(t, stateFile, h, intent)
	require.NoError(t, err)
	require.NoError(t, report.Error)
	return report
}

// handleBlockNodeIntent runs one block node install through BaseHandler the way
// a CLI command does: Setup's refresh, then HandleIntent, then the flush.
func handleBlockNodeIntent(t *testing.T, stateFile string, h *host, intent noopIntent) (*automa.Report, error) {
	t.Helper()
	return handleBlockNodeIntentWith(t, stateFile, h, intent, Teleport)
}

// handleBlockNodeIntentWith is handleBlockNodeIntent with the handler's managed components chosen.
func handleBlockNodeIntentWith(t *testing.T, stateFile string, h *host, intent noopIntent, managed ...Component) (*automa.Report, error) {
	t.Helper()
	return handleIntent(t, stateFile, h, intent, commonInputs(), owns(managed...))
}

// owns is the handler option that declares the components a test handler manages.
func owns(managed ...Component) BaseHandlerOption[struct{}] {
	return WithManagedComponents[struct{}](managed...)
}

// commonInputs is an interactive run with no override.
func commonInputs() models.CommonInputs {
	return models.CommonInputs{
		ExecutionOptions: models.WorkflowExecutionOptions{ExecutionMode: automa.StopOnError, RollbackMode: automa.StopOnError},
	}
}

// handleIntent is handleBlockNodeIntentWith with the common inputs and the
// handler's options chosen.
func handleIntent(t *testing.T, stateFile string, h *host, intent IntentHandler[struct{}], common models.CommonInputs, opts ...BaseHandlerOption[struct{}]) (*automa.Report, error) {
	t.Helper()
	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(handlerFileManager(newFileManager(t))))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())

	checkers := reality.Checkers{
		Cluster: funcChecker[state.ClusterState](func() (state.ClusterState, error) {
			return state.ClusterState{Created: true}, nil
		}),
		Machine: funcChecker[state.MachineState](func() (state.MachineState, error) {
			ms := sm.State().MachineState
			if h.software != nil {
				ms.Software = h.software
			}
			return ms, nil
		}),
		BlockNode: funcChecker[state.BlockNodeState](func() (state.BlockNodeState, error) {
			return sm.State().BlockNodeState, nil
		}),
		Consensus: funcChecker[map[string]state.ConsensusNodeState](func() (map[string]state.ConsensusNodeState, error) {
			return map[string]state.ConsensusNodeState{}, nil
		}),
		Teleport: funcChecker[state.TeleportState](func() (state.TeleportState, error) {
			return h.teleport, nil
		}),
	}
	if h.teleportProbe != nil {
		checkers.Teleport, err = reality.NewTeleportChecker(sm,
			func() (reality.HelmManager, error) { return noReleases{}, nil }, h.teleportProbe)
		require.NoError(t, err)
	}
	runtime, err := rsl.NewRuntimeResolver(models.Config{}, sm, checkers, time.Minute)
	require.NoError(t, err)
	base, err := NewBaseHandler[struct{}](runtime, models.TargetBlockNode, opts...)
	require.NoError(t, err)

	inputs := models.UserInputs[struct{}]{Common: common}
	return base.HandleIntent(context.Background(),
		models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}, inputs, intent, nil)
}

func configuredNodeAgent() state.TeleportState {
	return state.TeleportState{NodeAgent: state.TeleportNodeAgentState{Installed: true, Configured: true}}
}

func TestHandleIntent_NoOutOfBandChangeHasNoWarning(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())

	report := runBlockNodeIntent(t, stateFile, &host{teleport: configuredNodeAgent()}, noopIntent{})

	require.Empty(t, ui.CollectWarnings(report))
}

// A block node run reports a Teleport change, then flushes the live value as it
// always has, so the next run has nothing to report.
func TestHandleIntent_ReportsTeleportChangeThenPersistsLiveState(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())
	h := &host{teleport: configuredNodeAgent()}
	h.teleport.NodeAgent.Configured = false

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{})

	require.Equal(t, []string{
		`teleport nodeAgent.configured differs from persisted state: recorded "true", live "false"`,
	}, ui.CollectWarnings(report))
	require.Equal(t, h.teleport.NodeAgent, readState(t, stateFile).TeleportState.NodeAgent)

	second := runBlockNodeIntent(t, stateFile, h, noopIntent{})
	require.Empty(t, ui.CollectWarnings(second))
}

// A handler reports only the components it manages: with Teleport unmanaged, a
// Teleport change is neither reported nor absorbed — the persisted Teleport
// file is never touched, so its owning command can still report it later.
func TestHandleIntent_UnmanagedComponentIsNeitherReportedNorAbsorbed(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())
	teleportFile := state.ComponentFilePath(filepath.Dir(stateFile), state.ComponentTeleport)
	before, err := os.Stat(teleportFile)
	require.NoError(t, err)

	h := &host{teleport: configuredNodeAgent()}
	h.teleport.NodeAgent.Configured = false

	// Manage BlockNode only, so the live Teleport change is out of scope.
	report, err := handleBlockNodeIntentWith(t, stateFile, h, noopIntent{}, BlockNode)

	require.NoError(t, err)
	require.Empty(t, ui.CollectWarnings(report))
	// The out-of-band Teleport change is not folded into state: the persisted
	// baseline (Configured: true) is preserved, not the live value (false).
	require.Equal(t, configuredNodeAgent().NodeAgent, readState(t, stateFile).TeleportState.NodeAgent)
	// Stronger than content equality: the file itself was never rewritten.
	after, err := os.Stat(teleportFile)
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime(), "teleport.yaml must not be opened for writing by a BlockNode-only command")
}

func TestHandleIntent_WorkflowsOwnChangeIsNotReported(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, state.TeleportState{})
	h := &host{}

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{execute: func() { h.teleport = configuredNodeAgent() }})

	require.Empty(t, ui.CollectWarnings(report))
	require.Equal(t, configuredNodeAgent().NodeAgent, readState(t, stateFile).TeleportState.NodeAgent)
}

// With no state file there is no baseline, so a component already on the host
// is not an out-of-band change.
func TestHandleIntent_NoStateFileHasNoWarning(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")

	report := runBlockNodeIntent(t, stateFile, &host{teleport: configuredNodeAgent()}, noopIntent{})

	require.Empty(t, ui.CollectWarnings(report))
	require.Equal(t, configuredNodeAgent().NodeAgent, readState(t, stateFile).TeleportState.NodeAgent)
}

func installedClusterAgent() state.TeleportState {
	return state.TeleportState{ClusterAgent: state.TeleportClusterAgentState{
		Installed: true, Release: "teleport-agent", Namespace: "teleport-agent", ChartVersion: "18.6.4",
	}}
}

// The cluster runtime reaches the cluster, then the Teleport checker's own probe
// finds the API server silent. The persisted cluster agent stands, and the run
// reports that it could not read the agent, not a removal.
func TestHandleIntent_UnobservableClusterIsNotReportedAsRemoval(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, installedClusterAgent())
	h := &host{teleportProbe: func() reality.ClusterReachability { return reality.Unobservable }}

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{})

	require.Equal(t, []string{
		`teleport clusterAgent.installed could not be read from the live system: recorded "true"`,
	}, ui.CollectWarnings(report))
	require.Equal(t, installedClusterAgent().ClusterAgent, readState(t, stateFile).TeleportState.ClusterAgent)
}

// No kubeconfig is not proof the release is gone: nothing is reported.
func TestHandleIntent_NoKubeconfigIsNotReportedAsRemoval(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, installedClusterAgent())
	h := &host{teleportProbe: func() reality.ClusterReachability { return reality.NotConfigured }}

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{})

	require.Empty(t, ui.CollectWarnings(report))
	require.Equal(t, installedClusterAgent().ClusterAgent, readState(t, stateFile).TeleportState.ClusterAgent)
}

// A reachable cluster whose Helm releases lack the agent is a confirmed removal.
func TestHandleIntent_ReleaseMissingFromReachableClusterIsReportedAsRemoval(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, installedClusterAgent())
	h := &host{teleportProbe: func() reality.ClusterReachability { return reality.Reachable }}

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{})

	require.Equal(t, []string{
		`teleport clusterAgent.installed differs from persisted state: recorded "true", live "false"`,
	}, ui.CollectWarnings(report))
	require.Equal(t, state.TeleportClusterAgentState{}, readState(t, stateFile).TeleportState.ClusterAgent)
}

// captureLogs swaps the global logger for a buffer and restores it. Global
// state, so callers must not run in parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevLogger, prevLevel := *logx.As(), zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	logx.SetLogger(zerolog.New(&buf))
	t.Cleanup(func() {
		logx.SetLogger(prevLogger)
		zerolog.SetGlobalLevel(prevLevel)
	})
	return &buf
}

// A run that fails before its workflow has no report, but logs the change and
// flushes nothing, so the next successful run still reports it.
func TestHandleIntent_EarlyFailureLogsTheChangeAndKeepsTheBaseline(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())
	h := &host{teleport: configuredNodeAgent()}
	h.teleport.NodeAgent.Configured = false
	logs := captureLogs(t)

	_, err := handleBlockNodeIntent(t, stateFile, h, noopIntent{buildErr: errors.New("precondition failed")})

	require.Error(t, err)
	require.Contains(t, logs.String(), `"field":"nodeAgent.configured","persisted":"true","live":"false","message":"Live value differs from persisted state"`)
	require.True(t, readState(t, stateFile).TeleportState.NodeAgent.Configured, "nothing was flushed")

	report := runBlockNodeIntent(t, stateFile, h, noopIntent{})
	require.Equal(t, []string{
		`teleport nodeAgent.configured differs from persisted state: recorded "true", live "false"`,
	}, ui.CollectWarnings(report))
}

// TestHandleIntent_DisjointHandlersDoNotBlockEachOther runs two commands
// through the real HandleIntent path — not AcquireComponentLocks/FlushScoped
// called directly — so it actually exercises what lockedComponentIDs locks.
// A BlockNode-only handler holds its lock for the whole duration of a
// deliberately slow workflow; a concurrent Teleport-only handler must still
// complete quickly, because neither owns a file the other writes.
func TestHandleIntent_DisjointHandlersDoNotBlockEachOther(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())

	release := make(chan struct{})
	started := make(chan struct{})
	slow := noopIntent{execute: func() {
		close(started)
		<-release
	}}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := handleBlockNodeIntentWith(t, stateFile, &host{}, slow, BlockNode)
		require.NoError(t, err)
	}()

	<-started // the slow handler now holds its BlockNode lock

	fastDone := make(chan struct{})
	go func() {
		_, err := handleBlockNodeIntentWith(t, stateFile, &host{teleport: configuredNodeAgent()}, noopIntent{}, Teleport)
		require.NoError(t, err)
		close(fastDone)
	}()

	select {
	case <-fastDone:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("a Teleport-only handler must not block on a concurrent BlockNode-only handler's lock")
	}

	close(release)
	wg.Wait()
}

// TestOwnedComponentIDs_AreTheManagedComponentsOnce: the one declared set is
// what a handler locks and flushes, with a repeated component listed once, so
// AcquireComponentLocks never flocks the same file twice.
func TestOwnedComponentIDs_AreTheManagedComponentsOnce(t *testing.T) {
	h := BaseHandler[struct{}]{Managed: []Component{Teleport, System, Teleport}}

	require.Equal(t, []state.ComponentID{state.ComponentTeleport, state.ComponentSystem}, h.ownedComponentIDs())
	require.True(t, h.Owns(System))
	require.False(t, h.Owns(Profile))
}

// TestNewBaseHandler_ProfileExtractorRequiresTheProfileComponent: the extractor
// writes MachineState.Profile, which reaches disk only through the profile
// file, so a handler that would extract a profile it never flushes is refused.
func TestNewBaseHandler_ProfileExtractorRequiresTheProfileComponent(t *testing.T) {
	extract := WithProfileExtractor(func(struct{}) string { return "mainnet" })

	_, err := NewBaseHandler[struct{}](&rsl.RuntimeResolver{}, models.TargetBlockNode, extract, owns(BlockNode))
	require.Error(t, err)

	_, err = NewBaseHandler[struct{}](&rsl.RuntimeResolver{}, models.TargetBlockNode, extract, owns(BlockNode, Profile))
	require.NoError(t, err)
}

// TestHandleIntent_FailsFastOnEachOwnedLock: every component a handler owns is
// locked before the workflow runs, so another command holding any one of them
// stops this one at once, with nothing run and nothing written.
func TestHandleIntent_FailsFastOnEachOwnedLock(t *testing.T) {
	managed := []Component{BlockNode, Profile, Firewall, System}
	for _, c := range managed {
		t.Run(string(c.id), func(t *testing.T) {
			stateFile := filepath.Join(t.TempDir(), "state.yaml")
			writeState(t, stateFile, configuredNodeAgent())
			before := readState(t, stateFile)

			release, err := state.AcquireComponentLocks(filepath.Dir(stateFile), []state.ComponentID{c.id}, 0)
			require.NoError(t, err)
			defer release()

			ran := false
			_, err = handleBlockNodeIntentWith(t, stateFile, &host{}, noopIntent{execute: func() { ran = true }}, managed...)
			require.ErrorContains(t, err, string(c.id)+" is being modified")
			require.False(t, ran, "the workflow must not run while an owned component is locked")
			require.Equal(t, before.StateRecord, readState(t, stateFile).StateRecord, "nothing may be written")
		})
	}
}

// handleBlockNodeIntentWithCallback is handleBlockNodeIntentWith plus a flush
// callback. HandleIntent invokes the callback AFTER its pre-flush
// Runtime.Refresh (which captures this handler's hash baselines) and BEFORE
// FlushScoped writes its files — the exact seam where a second process could
// slip a write in underneath this one.
func handleBlockNodeIntentWithCallback(
	t *testing.T,
	stateFile string,
	h *host,
	intent noopIntent,
	callback func(full *state.State, effInputs models.UserInputs[struct{}]) error,
	managed ...Component,
) (*automa.Report, error) {
	t.Helper()
	sm, err := state.NewStateManager(state.WithState(state.NewState(stateFile)), state.WithFileManager(newFileManager(t)))
	require.NoError(t, err)
	require.NoError(t, sm.Refresh())

	checkers := reality.Checkers{
		Cluster: funcChecker[state.ClusterState](func() (state.ClusterState, error) {
			return state.ClusterState{Created: true}, nil
		}),
		Machine: funcChecker[state.MachineState](func() (state.MachineState, error) {
			return sm.State().MachineState, nil
		}),
		BlockNode: funcChecker[state.BlockNodeState](func() (state.BlockNodeState, error) {
			return sm.State().BlockNodeState, nil
		}),
		Consensus: funcChecker[map[string]state.ConsensusNodeState](func() (map[string]state.ConsensusNodeState, error) {
			return map[string]state.ConsensusNodeState{}, nil
		}),
		Teleport: funcChecker[state.TeleportState](func() (state.TeleportState, error) {
			return h.teleport, nil
		}),
	}
	runtime, err := rsl.NewRuntimeResolver(models.Config{}, sm, checkers, time.Minute)
	require.NoError(t, err)
	base, err := NewBaseHandler[struct{}](runtime, models.TargetBlockNode, WithManagedComponents[struct{}](managed...))
	require.NoError(t, err)

	inputs := models.UserInputs[struct{}]{Common: models.CommonInputs{
		ExecutionOptions: models.WorkflowExecutionOptions{ExecutionMode: automa.StopOnError, RollbackMode: automa.StopOnError},
	}}
	return base.HandleIntent(context.Background(),
		models.Intent{Action: models.ActionInstall, Target: models.TargetBlockNode}, inputs, intent, callback)
}

// TestHandleIntent_DisjointHandlersNeverContend: two commands that
// manage disjoint components never contend on a shared file. This used to fail
// (#1242): every command flushed the shared, unlocked machine.yaml, so a
// teleport command writing it inside a block node command's flush window made
// the block node command abort after its workflow had run. With machine.yaml
// split by owner (#1245), the teleport node command writes teleport.yaml and
// system.yaml, neither of which the block node command locks or writes.
//
// Handler A (BlockNode-only) blocks in its flush callback — AFTER
// HandleIntent's pre-flush Refresh captured A's baselines, BEFORE A's
// FlushScoped. While A is parked there, handler B (Teleport and System, like
// `teleport node install`) runs to completion. Two separate state managers
// (one per handler) model two separate processes with independent baselines.
func TestHandleIntent_DisjointHandlersNeverContend(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())

	aRefreshed := make(chan struct{}) // A has captured its baselines
	bDone := make(chan struct{})      // B has finished writing its files

	var aErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, aErr = handleBlockNodeIntentWithCallback(t, stateFile, &host{}, noopIntent{},
			func(_ *state.State, _ models.UserInputs[struct{}]) error {
				close(aRefreshed)
				<-bDone
				return nil
			},
			BlockNode)
	}()

	// Wait until A is parked between its pre-flush Refresh and its flush.
	select {
	case <-aRefreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("handler A never reached its flush callback")
	}

	// B, a teleport node command, installs the teleport binary and writes its
	// files, so the system record really changes under A.
	installedTeleport := map[string]state.SoftwareState{"teleport": {Name: "teleport", Installed: true}}
	_, bErr := handleBlockNodeIntentWith(t, stateFile,
		&host{teleport: configuredNodeAgent(), software: installedTeleport}, noopIntent{}, Teleport, System)
	require.NoError(t, bErr, "B (teleport node) should complete independently")

	close(bDone) // release A to flush
	wg.Wait()

	// A BlockNode-only command must not fail because a teleport node command
	// wrote its own files.
	require.NoError(t, aErr,
		"A (blocknode-only) must not fail just because B (teleport node) wrote teleport.yaml and system.yaml")

	// Confirm both components' own files reflect their writes.
	final := readState(t, stateFile)
	require.Equal(t, configuredNodeAgent().NodeAgent, final.TeleportState.NodeAgent)
	require.True(t, final.MachineState.Software["teleport"].Installed, "B's system record must have landed")
}

// TestFlushState_RecordsEntryOnEveryExitAfterTheWorkflow: once the workflow has
// run, the action is recorded however the flush ends: as succeeded or failed
// when its state landed, and marked NotPersisted when the refresh before the
// flush failed, so no state file was written.
func TestFlushState_RecordsEntryOnEveryExitAfterTheWorkflow(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	dir := filepath.Dir(stateFile)
	writeState(t, stateFile, configuredNodeAgent())
	h := &host{teleport: configuredNodeAgent()}

	runBlockNodeIntent(t, stateFile, h, noopIntent{})

	report, err := handleBlockNodeIntent(t, stateFile, h, noopIntent{stepErr: errors.New("step failed")})
	require.NoError(t, err, "a failed workflow is reported, and its state is still flushed")
	require.Error(t, report.Error)

	// The workflow breaks a state file, so the refresh before the flush fails.
	breakState := func() {
		require.NoError(t, os.WriteFile(state.ComponentFilePath(dir, state.ComponentTeleport), []byte("state: [broken"), 0o644))
	}
	_, err = handleBlockNodeIntent(t, stateFile, h, noopIntent{execute: breakState})
	require.ErrorContains(t, err, "failed to refresh runtime state before flush")

	entries := readActionHistory(t, dir)
	require.Len(t, entries, 3)
	require.Equal(t, state.OutcomeSucceeded, entries[0].Outcome)
	require.False(t, entries[0].NotPersisted)
	require.Equal(t, state.OutcomeFailed, entries[1].Outcome)
	require.False(t, entries[1].NotPersisted, "a failed workflow's state still landed")
	require.Equal(t, state.OutcomeSucceeded, entries[2].Outcome)
	require.True(t, entries[2].NotPersisted)
	require.Contains(t, entries[2].Error, "failed to refresh runtime state before flush")
	require.Empty(t, entries[2].Components)
}

// TestFlushState_LandedStateWithALostEntrySaysSo: when the state lands but its
// entry can't be appended, the command says exactly that, with the reason and
// a hint, rather than "failed to persist state".
func TestFlushState_LandedStateWithALostEntrySaysSo(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	dir := filepath.Dir(stateFile)
	writeState(t, stateFile, configuredNodeAgent())
	breakHistory := func() {
		require.NoError(t, os.Mkdir(state.ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails
	}

	_, err := handleBlockNodeIntent(t, stateFile, &host{teleport: configuredNodeAgent()}, noopIntent{execute: breakHistory})

	require.True(t, errorx.IsOfType(err, state.EntryNotAppendedError), "got %v", err)
	msg := doctor.OperatorMessage(err)
	require.Contains(t, msg, "the state was written but its action history entry was not appended")
	require.NotContains(t, msg, "failed to persist state")
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok)
	require.Equal(t, reasons.StateOperationFailed, reason)
	hints, ok := errx.Hints(err)
	require.True(t, ok)
	require.Contains(t, hints, "Check that "+state.ActionHistoryFilePath(dir)+" is writable.")
}

// TestFlushState_FailedWorkflowKeepsItsFailureWhenRecordingHasTrouble: a
// failed workflow whose state landed returns its report, with the step's own
// failure, and no error, whether its entry was lost or appended but not
// synced; the trouble recording it is a warning.
func TestFlushState_FailedWorkflowKeepsItsFailureWhenRecordingHasTrouble(t *testing.T) {
	for name, tc := range map[string]struct {
		fm      func(fsx.Manager) fsx.Manager
		history func(dir string)
		warning string
	}{
		"entry lost": {
			history: func(dir string) {
				require.NoError(t, os.Mkdir(state.ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails
			},
			warning: "action history entry was not appended",
		},
		"entry not synced": {
			fm:      func(fm fsx.Manager) fsx.Manager { return unsyncedAppender{fm} },
			warning: "action history entry was appended but not synced to disk",
		},
	} {
		t.Run(name, func(t *testing.T) {
			state.TakeWarnings()
			t.Cleanup(func() { state.TakeWarnings() })
			stateFile := filepath.Join(t.TempDir(), "state.yaml")
			dir := filepath.Dir(stateFile)
			writeState(t, stateFile, configuredNodeAgent())
			if tc.fm != nil {
				t.Cleanup(func() { handlerFileManager = func(fm fsx.Manager) fsx.Manager { return fm } })
				handlerFileManager = tc.fm
			}
			execute := func() {}
			if tc.history != nil {
				execute = func() { tc.history(dir) }
			}

			report, err := handleBlockNodeIntent(t, stateFile, &host{teleport: configuredNodeAgent()},
				noopIntent{execute: execute, stepErr: errors.New("kubeadm init failed")})

			require.NoError(t, err)
			require.NotNil(t, report)
			require.Error(t, report.Error)
			require.Len(t, report.StepReports, 1)
			require.ErrorContains(t, report.StepReports[0].Error, "kubeadm init failed", "the step's own failure reaches the summary")
			warnings := state.TakeWarnings()
			require.Len(t, warnings, 1)
			require.Contains(t, warnings[0], tc.warning)
		})
	}
}

// TestFlushState_FailedVersionStampIsNotPersisted: a workflow whose version
// stamp could not write provisioner.yaml records its entry NotPersisted, saying
// the version was not recorded.
func TestFlushState_FailedVersionStampIsNotPersisted(t *testing.T) {
	t.Cleanup(models.SetPaths(t.TempDir()))
	versionDir := models.Paths().StateDir
	require.NoError(t, os.MkdirAll(versionDir, 0o755))
	release, err := state.AcquireProvisionerLock(versionDir, 0)
	require.NoError(t, err)
	defer release()
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())

	_, err = handleBlockNodeIntent(t, stateFile, &host{teleport: configuredNodeAgent()}, noopIntent{recordVersion: true})

	require.NoError(t, err)
	entries := readActionHistory(t, filepath.Dir(stateFile))
	require.Len(t, entries, 1)
	require.Equal(t, state.OutcomeSucceeded, entries[0].Outcome)
	require.True(t, entries[0].NotPersisted)
	require.Equal(t, "the provisioner version was not recorded; startup migrations may run again on the next invocation", entries[0].Error)
}

// TestFlushState_FailedCallbackIsRecordedAsNotPersisted: a callback that fails
// after the workflow leaves its state unwritten, so the entry is appended
// marked NotPersisted, with the callback's error.
func TestFlushState_FailedCallbackIsRecordedAsNotPersisted(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	writeState(t, stateFile, configuredNodeAgent())

	_, err := handleBlockNodeIntentWithCallback(t, stateFile, &host{teleport: configuredNodeAgent()}, noopIntent{},
		func(*state.State, models.UserInputs[struct{}]) error { return errors.New("cannot patch") })

	require.ErrorContains(t, err, "cannot patch")
	entries := readActionHistory(t, filepath.Dir(stateFile))
	require.Len(t, entries, 1)
	require.Equal(t, state.OutcomeSucceeded, entries[0].Outcome)
	require.True(t, entries[0].NotPersisted)
	require.Contains(t, entries[0].Error, "cannot patch")
	require.Empty(t, entries[0].Components)
}

// TestFlushState_LostEntryOfAFailedFlushIsAWarning: when the refresh after the
// workflow fails and the entry can't be appended either, the command returns
// the flush's own error, and the lost entry is a state warning.
func TestFlushState_LostEntryOfAFailedFlushIsAWarning(t *testing.T) {
	state.TakeWarnings()
	t.Cleanup(func() { state.TakeWarnings() })
	stateFile := filepath.Join(t.TempDir(), "state.yaml")
	dir := filepath.Dir(stateFile)
	writeState(t, stateFile, configuredNodeAgent())
	breakStateAndHistory := func() {
		require.NoError(t, os.WriteFile(state.ComponentFilePath(dir, state.ComponentTeleport), []byte("state: [broken"), 0o644))
		require.NoError(t, os.Mkdir(state.ActionHistoryFilePath(dir), 0o755)) // appending to a directory fails
	}

	_, err := handleBlockNodeIntent(t, stateFile, &host{teleport: configuredNodeAgent()}, noopIntent{execute: breakStateAndHistory})

	require.ErrorContains(t, err, "failed to refresh runtime state before flush")
	require.NotContains(t, err.Error(), "action history")
	require.NotContains(t, doctor.OperatorMessage(err), "{reason:")
	warnings := state.TakeWarnings()
	require.Len(t, warnings, 1)
	require.Contains(t, warnings[0], "action history entry was not appended")
}

// A change the run was allowed to destroy is marked unconfirmed when the read
// after the workflow failed; the other records stand as found.
func TestOwnedDriftRun_UnconfirmedMarksOnlyAllowedDestroys(t *testing.T) {
	run := ownedDriftRun{records: []state.OwnedDriftRecord{
		{Component: "teleport", Field: "nodeAgent.configured", DestroyAllowed: true},
		{Component: "teleport", Field: "clusterAgent.chartVersion"},
	}}

	got := run.unconfirmed()

	require.True(t, got[0].Unconfirmed)
	require.False(t, got[0].Destroyed)
	require.False(t, got[1].Unconfirmed)
	require.False(t, run.records[0].Unconfirmed, "the run's own records are not changed")
	require.Empty(t, ownedDriftRun{}.unconfirmed())
}
