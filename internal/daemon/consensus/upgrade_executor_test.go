// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package consensus

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/daemonkit/eventlog"
	cn "github.com/hashgraph/solo-weaver/internal/consensus"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

const (
	testNS     = "hedera-network"
	testNodeID = "0.0.3"
	// testOperationID is the real operationId shape: node<N>-upgrade-<version>-<timestamp>.
	testOperationID = "node0-upgrade-v0.75.0-20260522T120000Z"
	testCRName      = "cr"
)

func newFakeDynamicClient(t *testing.T, objects ...runtime.Object) *fake.FakeDynamicClient {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "operator.solo.hedera.com", Version: "v1alpha1", Kind: "NetworkUpgradeExecute"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "operator.solo.hedera.com", Version: "v1alpha1", Kind: "NetworkUpgradeExecuteList"},
		&unstructured.UnstructuredList{},
	)
	return fake.NewSimpleDynamicClient(scheme, objects...)
}

func newExecuteCR(name, operationID, phase string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "operator.solo.hedera.com/v1alpha1",
		"kind":       "NetworkUpgradeExecute",
		"metadata":   map[string]interface{}{"name": name, "namespace": testNS},
		"spec":       map[string]interface{}{"operationId": operationID, "orbit": testNS},
		"status":     map[string]interface{}{"phase": phase},
	}}
}

// readReasons returns the reason= field of every JSONL line in path, in order.
func readReasons(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var reasons []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e struct {
			Reason string `json:"reason"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &e))
		reasons = append(reasons, e.Reason)
	}
	return reasons
}

// conditions returns the CR's status.conditions as maps.
func conditions(t *testing.T, client *fake.FakeDynamicClient, crName string) []map[string]interface{} {
	t.Helper()
	obj, err := client.Resource(networkUpgradeExecuteGVR).Namespace(testNS).Get(context.Background(), crName, metav1.GetOptions{})
	require.NoError(t, err)
	raw, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	out := make([]map[string]interface{}, 0, len(raw))
	for _, r := range raw {
		if c, ok := r.(map[string]interface{}); ok {
			out = append(out, c)
		}
	}
	return out
}

func condition(t *testing.T, client *fake.FakeDynamicClient, crName, condType string) map[string]interface{} {
	t.Helper()
	for _, c := range conditions(t, client, crName) {
		if tt, _, _ := unstructured.NestedString(c, "type"); tt == condType {
			return c
		}
	}
	return nil
}

func conditionStatus(t *testing.T, client *fake.FakeDynamicClient, crName, condType string) string {
	t.Helper()
	c := condition(t, client, crName, condType)
	require.NotNil(t, c, "condition %s should be present", condType)
	s, _, _ := unstructured.NestedString(c, "status")
	return s
}

func newUpgradeMonitorForTest(t *testing.T, eventsDir string, objects ...runtime.Object) *UpgradeMonitor {
	t.Helper()
	return NewUpgradeMonitorWithClient(UpgradeMonitorConfig{
		Namespace:        testNS,
		NodeID:           testNodeID,
		UpgradeEventsDir: eventsDir,
	}, newFakeDynamicClient(t, objects...))
}

func TestBuildWorkflow_StepOrder(t *testing.T) {
	report := automa.RunWorkflow(context.Background(), (&upgradeExecutor{}).buildWorkflow())
	require.True(t, report.IsSuccess(), "all stub steps should succeed: %v", report.Error)

	got := make([]string, len(report.StepReports))
	for i, r := range report.StepReports {
		got[i] = r.Id
	}
	assert.Equal(t, []string{
		"external-files",
		"infra-versions-placement",
		"runtime-safety-gate",
		"infra-upgrade-detect",
		"create-config-crs",
		"wait-config-reconcile",
	}, got)
}

func TestSetExecuteCondition_UpsertAndIdempotent(t *testing.T) {
	cr := newExecuteCR(testCRName, testOperationID, string(cn.PhaseReadyForProvisionerDaemon))
	client := newFakeDynamicClient(t, cr)
	ctx := context.Background()

	require.NoError(t, setExecuteCondition(ctx, client, testNS, testCRName,
		cn.DaemonResultCondition, cn.ConditionTrue, cn.ReasonDaemonSucceeded, "ok"))
	assert.Equal(t, "True", conditionStatus(t, client, testCRName, "DaemonResult"))
	ltt1, _, _ := unstructured.NestedString(condition(t, client, testCRName, "DaemonResult"), "lastTransitionTime")

	// Re-run with the same status: idempotent — single entry, timestamp preserved.
	require.NoError(t, setExecuteCondition(ctx, client, testNS, testCRName,
		cn.DaemonResultCondition, cn.ConditionTrue, cn.ReasonDaemonSucceeded, "ok"))
	assert.Len(t, conditions(t, client, testCRName), 1)
	ltt2, _, _ := unstructured.NestedString(condition(t, client, testCRName, "DaemonResult"), "lastTransitionTime")
	assert.Equal(t, ltt1, ltt2, "lastTransitionTime must not churn when status is unchanged")

	// status.phase is left untouched (operator owns it).
	obj, _ := client.Resource(networkUpgradeExecuteGVR).Namespace(testNS).Get(ctx, testCRName, metav1.GetOptions{})
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	assert.Equal(t, string(cn.PhaseReadyForProvisionerDaemon), phase)
}

func phaseOf(t *testing.T, client *fake.FakeDynamicClient, crName string) string {
	t.Helper()
	obj, err := client.Resource(networkUpgradeExecuteGVR).Namespace(testNS).Get(context.Background(), crName, metav1.GetOptions{})
	require.NoError(t, err)
	p, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	return p
}

func TestReportOutcome_SuccessSetsConditionsAndAdvancesPhase(t *testing.T) {
	cr := newExecuteCR(testCRName, testOperationID, string(cn.PhaseReadyForProvisionerDaemon))
	client := newFakeDynamicClient(t, cr)
	x := &upgradeExecutor{client: client, namespace: testNS, crName: testCRName, sink: newEventSink(nil, testNodeID, testOperationID)}

	require.NoError(t, x.reportOutcome(context.Background(), nil))
	assert.Equal(t, "True", conditionStatus(t, client, testCRName, "ConfigCRsApplied"))
	assert.Equal(t, "True", conditionStatus(t, client, testCRName, "DaemonResult"))
	assert.Equal(t, string(cn.PhasePendingNodeUpgrade), phaseOf(t, client, testCRName), "daemon advances to PendingNodeUpgrade on success")
}

func TestReportOutcome_FailureSetsDaemonResultFalseNoPhaseAdvance(t *testing.T) {
	cr := newExecuteCR(testCRName, testOperationID, string(cn.PhaseReadyForProvisionerDaemon))
	client := newFakeDynamicClient(t, cr)
	x := &upgradeExecutor{client: client, namespace: testNS, crName: testCRName, sink: newEventSink(nil, testNodeID, testOperationID)}

	require.NoError(t, x.reportOutcome(context.Background(), errorx.IllegalState.New("boom")))
	assert.Equal(t, "False", conditionStatus(t, client, testCRName, "DaemonResult"))
	assert.Nil(t, condition(t, client, testCRName, "ConfigCRsApplied"), "ConfigCRsApplied must not be set on failure")
	assert.Equal(t, string(cn.PhaseReadyForProvisionerDaemon), phaseOf(t, client, testCRName), "daemon must not advance the phase on failure")
}

func TestPatchExecutePhase_WritesDaemonWritablePhase(t *testing.T) {
	cr := newExecuteCR(testCRName, testOperationID, string(cn.PhaseReadyForProvisionerDaemon))
	client := newFakeDynamicClient(t, cr)

	require.NoError(t, patchExecutePhase(context.Background(), client, testNS, testCRName, cn.PhasePendingNodeUpgrade))
	assert.Equal(t, string(cn.PhasePendingNodeUpgrade), phaseOf(t, client, testCRName))
}

func TestPatchExecutePhase_RejectsNonDaemonWritablePhase(t *testing.T) {
	cr := newExecuteCR(testCRName, testOperationID, string(cn.PhaseReadyForProvisionerDaemon))
	client := newFakeDynamicClient(t, cr)

	err := patchExecutePhase(context.Background(), client, testNS, testCRName, cn.PhaseSucceeded)
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, errorx.AssertionFailed), "terminal phase write must be an invariant breach")
	assert.Equal(t, string(cn.PhaseReadyForProvisionerDaemon), phaseOf(t, client, testCRName), "CR untouched")
}

func TestRunExecuteWorkflow_SuccessEmitsStartedCompleted(t *testing.T) {
	dir := t.TempDir()
	logger, err := eventlog.NewOperation(dir, testOperationID)
	require.NoError(t, err)
	sink := newEventSink(logger, testNodeID, testOperationID)

	passing := automa.NewWorkflowBuilder().WithId("t").WithExecutionMode(automa.StopOnError).
		Steps(timedStep("noop", time.Second, func(context.Context) error { return nil }))

	require.NoError(t, runExecuteWorkflow(context.Background(), sink, passing))
	sink.close()

	assert.Equal(t, []string{
		ReasonExecuteWorkflowStarted.String(),
		ReasonExecuteWorkflowCompleted.String(),
	}, readReasons(t, filepath.Join(dir, "consensus-"+testOperationID+".jsonl")))
}

func TestRunExecuteWorkflow_FailureEmitsStartedFailed(t *testing.T) {
	dir := t.TempDir()
	logger, err := eventlog.NewOperation(dir, testOperationID)
	require.NoError(t, err)
	sink := newEventSink(logger, testNodeID, testOperationID)

	failing := automa.NewWorkflowBuilder().WithId("t").WithExecutionMode(automa.StopOnError).
		Steps(timedStep("boom", time.Second, func(context.Context) error {
			return errorx.IllegalState.New("boom")
		}))

	require.Error(t, runExecuteWorkflow(context.Background(), sink, failing))
	sink.close()

	assert.Equal(t, []string{
		ReasonExecuteWorkflowStarted.String(),
		ReasonExecuteWorkflowFailed.String(),
	}, readReasons(t, filepath.Join(dir, "consensus-"+testOperationID+".jsonl")))
}

func TestHandleExecute_EndToEnd_SetsConditionsEmitsAndPrunes(t *testing.T) {
	dir := t.TempDir()
	staleName := "consensus-node0-upgrade-v0.60.0-20200101T000000Z.jsonl"
	require.NoError(t, os.WriteFile(filepath.Join(dir, staleName), []byte("{}\n"), 0o640))

	cr := newExecuteCR(testCRName, testOperationID, string(cn.PhaseReadyForProvisionerDaemon))
	um := newUpgradeMonitorForTest(t, dir, cr)

	require.NoError(t, um.handleExecute(context.Background(), cr))

	// Lifecycle events landed in the per-op JSONL.
	assert.Equal(t, []string{
		ReasonExecuteWorkflowStarted.String(),
		ReasonExecuteWorkflowCompleted.String(),
	}, readReasons(t, filepath.Join(dir, "consensus-"+testOperationID+".jsonl")))

	// Handshake: conditions set and the daemon advanced to PendingNodeUpgrade.
	client := um.client.(*fake.FakeDynamicClient)
	assert.Equal(t, "True", conditionStatus(t, client, testCRName, "DaemonResult"))
	assert.Equal(t, "True", conditionStatus(t, client, testCRName, "ConfigCRsApplied"))
	assert.Equal(t, string(cn.PhasePendingNodeUpgrade), phaseOf(t, client, testCRName))

	// The aged log was pruned after the operation.
	_, err := os.Stat(filepath.Join(dir, staleName))
	assert.True(t, os.IsNotExist(err), "stale log should be pruned")
}

func TestRunExecute_NilSafeWhenEventsDirUnset(t *testing.T) {
	cr := newExecuteCR(testCRName, testOperationID, string(cn.PhaseReadyForProvisionerDaemon))
	um := newUpgradeMonitorForTest(t, "", cr) // no events dir ⇒ logx-only sink
	assert.NoError(t, um.handleExecute(context.Background(), cr))
	assert.Equal(t, "True", conditionStatus(t, um.client.(*fake.FakeDynamicClient), testCRName, "DaemonResult"))
}
