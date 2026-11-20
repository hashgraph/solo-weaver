// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// fakeRunStateClient returns capsule reads in order (the last one repeats) and
// records each apply.
type fakeRunStateClient struct {
	reads   []map[string]any // nil entry = capsule not found
	readIdx int
	readErr error // when set, every capsule read fails
	genesis bool

	existsErr   error
	existsCalls int

	appliedManager string
	applied        *unstructured.Unstructured
}

func (f *fakeRunStateClient) ResourceExists(_ context.Context, _, kind, _, _ string) (bool, error) {
	f.existsCalls++
	if f.existsErr != nil {
		return false, f.existsErr
	}
	return kind == "ConfigMap" && f.genesis, nil
}

func (f *fakeRunStateClient) GetResourceNestedMap(context.Context, string, string, string, string, ...string) (map[string]any, bool, error) {
	if f.readErr != nil {
		return nil, false, f.readErr
	}
	if len(f.reads) == 0 {
		return nil, false, nil
	}
	obj := f.reads[min(f.readIdx, len(f.reads)-1)]
	f.readIdx++
	return obj, obj != nil, nil
}

func (f *fakeRunStateClient) ApplyFieldsAs(_ context.Context, fieldManager string, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	f.appliedManager = fieldManager
	f.applied = obj
	return obj, nil
}

func (f *fakeRunStateClient) provider() RunStateKubeProvider {
	return func(context.Context) (RunStateKubeClient, error) { return f, nil }
}

func capsuleObj(gen, observed int64, spec map[string]any, phase, runState string) map[string]any {
	status := map[string]any{"observedGeneration": observed}
	if phase != "" {
		status["phase"] = phase
	}
	if runState != "" {
		status["runState"] = runState
	}
	return map[string]any{
		"metadata": map[string]any{"generation": gen},
		"spec":     spec,
		"status":   status,
	}
}

func runStateInputs() models.ConsensusNodeInputs {
	return models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns", NodeId: 0}
}

func runRunStateStep(t *testing.T, b automa.Builder) *automa.Report {
	t.Helper()
	step, err := b.Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func TestConsensusStopIntended(t *testing.T) {
	tests := []struct {
		policy, desired string
		want            bool
	}{
		{"", "", false},
		{"Auto", "", false},
		{"Auto", "Running", false},
		{"Auto", "Stopped", true},
		{"Manual", "", true},
		{"Manual", "Running", false},
		{"Manual", "Stopped", true},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, consensusStopIntended(tt.policy, tt.desired), "policy=%q desired=%q", tt.policy, tt.desired)
	}
}

func TestSetConsensusRunState_AppliesOnlyDesiredRunState(t *testing.T) {
	fake := &fakeRunStateClient{}
	rpt := runRunStateStep(t, SetConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateStopped, fake.provider()))
	require.Equal(t, automa.StatusSuccess, rpt.Status)

	require.Equal(t, ConsensusRunStateFieldManager, fake.appliedManager)
	require.NotEqual(t, "solo-weaver", fake.appliedManager, "install's manager would drop the rest of the spec")
	spec, found, err := unstructured.NestedMap(fake.applied.Object, "spec")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, map[string]any{"desiredRunState": "Stopped"}, spec)
	require.Equal(t, models.ConsensusCapsuleName("ns", 0), fake.applied.GetName())
	require.Equal(t, "ns", fake.applied.GetNamespace())
	require.NotContains(t, fake.applied.Object, "status")
}

func TestPrecheckConsensusRunState_NotInstalled(t *testing.T) {
	for _, desired := range []operatorv1alpha1.RunState{operatorv1alpha1.RunStateRunning, operatorv1alpha1.RunStateStopped} {
		fake := &fakeRunStateClient{genesis: true}
		rpt := runRunStateStep(t, PrecheckConsensusRunState(runStateInputs(), desired, fake.provider()))
		require.Equal(t, automa.StatusFailed, rpt.Status, string(desired))
		require.Contains(t, rpt.Error.Error(), "not installed")
	}
}

func TestPrecheckConsensusRunState_StartRefusesOffline(t *testing.T) {
	fake := &fakeRunStateClient{genesis: true, reads: []map[string]any{
		capsuleObj(1, 1, map[string]any{"offline": true}, "Pending", ""),
	}}
	rpt := runRunStateStep(t, PrecheckConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateRunning, fake.provider()))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	require.Contains(t, rpt.Error.Error(), "offline")
}

func TestPrecheckConsensusRunState_StartRefusesWithoutGenesis(t *testing.T) {
	fake := &fakeRunStateClient{reads: []map[string]any{
		capsuleObj(1, 1, map[string]any{}, "Pending", ""),
	}}
	rpt := runRunStateStep(t, PrecheckConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateRunning, fake.provider()))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	require.Contains(t, rpt.Error.Error(), "genesis")
}

func TestPrecheckConsensusRunState_GenesisReadErrorNamesTheConfigMap(t *testing.T) {
	fake := &fakeRunStateClient{existsErr: errors.New("boom"), reads: []map[string]any{
		capsuleObj(1, 1, map[string]any{}, "Pending", ""),
	}}
	rpt := runRunStateStep(t, PrecheckConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateRunning, fake.provider()))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	require.Contains(t, rpt.Error.Error(), "failed to read ConfigMap "+NetworkGenesisConfigMapName)
	hints, ok := errx.Hints(rpt.Error)
	require.True(t, ok)
	require.Contains(t, strings.Join(hints, "\n"), "get configmap "+NetworkGenesisConfigMapName)
	require.NotContains(t, strings.Join(hints, "\n"), "consensuscapsule")
}

func TestPrecheckConsensusRunState_StopAllowedWithoutGenesisOrWhenOffline(t *testing.T) {
	fake := &fakeRunStateClient{reads: []map[string]any{
		capsuleObj(1, 1, map[string]any{"offline": true}, "Pending", ""),
	}}
	rpt := runRunStateStep(t, PrecheckConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateStopped, fake.provider()))
	require.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestPrecheckConsensusRunState_NoStartPolicyGate(t *testing.T) {
	for _, policy := range []string{"Auto", "Manual"} {
		for _, desired := range []operatorv1alpha1.RunState{operatorv1alpha1.RunStateRunning, operatorv1alpha1.RunStateStopped} {
			fake := &fakeRunStateClient{genesis: true, reads: []map[string]any{
				capsuleObj(1, 1, map[string]any{"startPolicy": policy}, "Running", "Running"),
			}}
			rpt := runRunStateStep(t, PrecheckConsensusRunState(runStateInputs(), desired, fake.provider()))
			require.Equal(t, automa.StatusSuccess, rpt.Status, "policy=%s desired=%s", policy, desired)
		}
	}
}

func withFastPoll(t *testing.T) {
	t.Helper()
	old := consensusRunStatePollInterval
	consensusRunStatePollInterval = 5 * time.Millisecond
	t.Cleanup(func() { consensusRunStatePollInterval = old })
}

// The stale read still shows Active from before the stop; the wait must not
// accept anything until observedGeneration catches up.
func TestWaitConsensusRunState_StopIgnoresStaleStatus(t *testing.T) {
	withFastPoll(t)
	stopped := map[string]any{"desiredRunState": "Stopped"}
	fake := &fakeRunStateClient{genesis: true, reads: []map[string]any{
		capsuleObj(3, 2, stopped, "Active", "Stopped"), // stale: old generation
		capsuleObj(3, 3, stopped, "Active", "Running"),
		capsuleObj(3, 3, stopped, "Active", "Stopped"), // s6 down, platform still up
		capsuleObj(3, 3, stopped, "Stopped", "Stopped"),
	}}
	rpt := runRunStateStep(t, WaitConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateStopped, time.Second, fake.provider()))
	require.Equal(t, automa.StatusSuccess, rpt.Status)
	require.Equal(t, 4, fake.readIdx)
	require.Equal(t, "Stopped", rpt.Metadata["runState"])
	require.Equal(t, "Stopped", rpt.Metadata["phase"])
}

// #1479: s6 reports Stopped while java runs on and the phase stays Active. The
// stop must time out and say so, not report success.
func TestWaitConsensusRunState_StopNeedsPhaseStopped(t *testing.T) {
	withFastPoll(t)
	fake := &fakeRunStateClient{genesis: true, reads: []map[string]any{
		capsuleObj(2, 2, map[string]any{"desiredRunState": "Stopped"}, "Active", "Stopped"),
	}}
	rpt := runRunStateStep(t, WaitConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateStopped, 30*time.Millisecond, fake.provider()))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	require.Contains(t, rpt.Error.Error(), "timed out")
	hints, ok := errx.Hints(rpt.Error)
	require.True(t, ok)
	require.Contains(t, strings.Join(hints, "\n"), "runState=Stopped but phase=Active")
}

func TestWaitConsensusRunState_StartWaitsForRunningOrActive(t *testing.T) {
	withFastPoll(t)
	running := map[string]any{"desiredRunState": "Running"}
	fake := &fakeRunStateClient{genesis: true, reads: []map[string]any{
		capsuleObj(4, 3, running, "Running", "Running"), // stale
		capsuleObj(4, 4, running, "Stopped", "Stopped"),
		capsuleObj(4, 4, running, "Running", "Running"),
	}}
	rpt := runRunStateStep(t, WaitConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateRunning, time.Second, fake.provider()))
	require.Equal(t, automa.StatusSuccess, rpt.Status)
	require.Equal(t, "Running", rpt.Metadata["phase"])
}

func TestWaitConsensusRunState_FailsFastOnFailed(t *testing.T) {
	withFastPoll(t)
	fake := &fakeRunStateClient{genesis: true, reads: []map[string]any{
		capsuleObj(2, 2, map[string]any{"desiredRunState": "Running"}, "Failed", ""),
	}}
	rpt := runRunStateStep(t, WaitConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateRunning, 5*time.Second, fake.provider()))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	require.Contains(t, rpt.Error.Error(), "Failed")
}

func TestWaitConsensusRunState_TimesOut(t *testing.T) {
	withFastPoll(t)
	fake := &fakeRunStateClient{genesis: true, reads: []map[string]any{
		capsuleObj(2, 2, map[string]any{"desiredRunState": "Running"}, "Pending", ""),
	}}
	rpt := runRunStateStep(t, WaitConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateRunning, 30*time.Millisecond, fake.provider()))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	require.Contains(t, rpt.Error.Error(), "timed out")
}

func TestWaitConsensusRunState_ZeroTimeoutReportsWithoutWait(t *testing.T) {
	fake := &fakeRunStateClient{genesis: true, reads: []map[string]any{
		capsuleObj(2, 1, map[string]any{"desiredRunState": "Running"}, "Stopped", "Stopped"),
	}}
	rpt := runRunStateStep(t, WaitConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateRunning, 0, fake.provider()))
	require.Equal(t, automa.StatusSuccess, rpt.Status)
	require.Equal(t, 1, fake.readIdx)
}

func TestWaitConsensusRunState_StopBeforeGenesisDoesNotWait(t *testing.T) {
	fake := &fakeRunStateClient{genesis: false, reads: []map[string]any{
		capsuleObj(2, 1, map[string]any{"desiredRunState": "Stopped"}, "Pending", ""),
	}}
	rpt := runRunStateStep(t, WaitConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateStopped, time.Second, fake.provider()))
	require.Equal(t, automa.StatusSuccess, rpt.Status)
}

func TestWaitConsensusRunState_StopReadsGenesisOnce(t *testing.T) {
	withFastPoll(t)
	stopped := map[string]any{"desiredRunState": "Stopped"}
	fake := &fakeRunStateClient{genesis: true, reads: []map[string]any{
		capsuleObj(3, 3, stopped, "Active", "Running"),
		capsuleObj(3, 3, stopped, "Active", "Stopped"),
		capsuleObj(3, 3, stopped, "Stopped", "Stopped"),
	}}
	rpt := runRunStateStep(t, WaitConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateStopped, time.Second, fake.provider()))
	require.Equal(t, automa.StatusSuccess, rpt.Status)
	require.Equal(t, 3, fake.readIdx)
	require.Equal(t, 1, fake.existsCalls)
}

func TestWaitConsensusRunState_TimeoutReportsLastReadError(t *testing.T) {
	withFastPoll(t)
	fake := &fakeRunStateClient{genesis: true, readErr: errors.New("connection refused")}
	rpt := runRunStateStep(t, WaitConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateRunning, 30*time.Millisecond, fake.provider()))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	require.Contains(t, rpt.Error.Error(), "timed out")
	require.Contains(t, rpt.Error.Error(), "connection refused")
}

func TestWaitConsensusRunState_CapsuleDeleted(t *testing.T) {
	fake := &fakeRunStateClient{genesis: true, reads: []map[string]any{nil}}
	rpt := runRunStateStep(t, WaitConsensusRunState(runStateInputs(), operatorv1alpha1.RunStateStopped, time.Second, fake.provider()))
	require.Equal(t, automa.StatusFailed, rpt.Status)
}
