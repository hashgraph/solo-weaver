// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
)

const lostDataHint = "--recreate-pvcs already deleted these PVCs and their data cannot be restored: "

// lostDataHintOf returns the lost-data hint of err, or "" when it has none.
func lostDataHintOf(err error) string {
	for _, h := range hintsOf(err) {
		if strings.HasPrefix(h, lostDataHint) {
			return h
		}
	}
	return ""
}

// fastCapsuleRetry removes the wait between apply retries for the test.
func fastCapsuleRetry(t *testing.T) {
	t.Helper()
	old := capsuleApplyBackoff
	capsuleApplyBackoff = wait.Backoff{Steps: old.Steps}
	t.Cleanup(func() { capsuleApplyBackoff = old })
}

func TestConsensusCapsuleDryRun_PassesAndSavesNothing(t *testing.T) {
	fake := &fakeCapsuleClient{existing: map[string]string{}}
	in := secretInputs()

	require.NoError(t, ConsensusCapsuleDryRun(in, fake.provider())(context.Background()))
	assert.Equal(t, []string{models.ConsensusCapsuleName(in.OrbitName, in.NodeId)}, fake.dryRuns)
	assert.Empty(t, fake.applied, "a dry-run must not save the capsule")
}

func TestConsensusCapsuleDryRun_RejectionCarriesAdmissionText(t *testing.T) {
	fake := &fakeCapsuleClient{
		existing:  map[string]string{},
		dryRunErr: errorx.RejectedOperation.New("spec.orbit must match the namespace"),
	}

	err := ConsensusCapsuleDryRun(secretInputs(), fake.provider())(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec.orbit must match the namespace")
	assert.Contains(t, err.Error(), "no PVC was changed")
	assert.NotEmpty(t, hintsOf(err))
	reason, ok := errx.ReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, reasons.PreconditionNotMet, reason)
	assert.Empty(t, fake.applied)
}

// The dry-run and the create step send the same capsule.
func TestConsensusCapsuleDryRun_SameCapsuleAsCreate(t *testing.T) {
	in := secretInputs()
	dry := &fakeCapsuleClient{existing: map[string]string{}}
	require.NoError(t, ConsensusCapsuleDryRun(in, dry.provider())(context.Background()))

	live := &fakeCapsuleClient{existing: map[string]string{}}
	rpt := runCreateCapsule(t, live, in)
	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)

	assert.Equal(t, findCapsule(t, live.appliedObjs), findCapsule(t, dry.dryRunObjs))
}

func deletedRecord(names ...string) *DeletedPVCs {
	d := &DeletedPVCs{}
	for _, n := range names {
		d.add(n)
	}
	return d
}

func TestCreateConsensusCapsule_ApplyFailureAfterDeleteNamesLostPVCs(t *testing.T) {
	in := secretInputs()
	in.RecreatePVCs = true
	fake := &fakeCapsuleClient{existing: map[string]string{}, applyErrs: []error{errors.New("connection refused")}}

	rpt := runCreateCapsuleWithDeleted(t, fake, in, deletedRecord("pvc-a", "pvc-b"))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Equal(t, lostDataHint+"pvc-a, pvc-b", lostDataHintOf(rpt.Error))
	assert.Contains(t, hintsOf(rpt.Error), "Nothing was installed. Fix the cause, then re-run the same install command; it creates the node with new PVCs")
}

// --recreate-pvcs without a deleted PVC must not claim lost data.
func TestCreateConsensusCapsule_ApplyFailureWithNoDeletedPVCHasNoLostDataHint(t *testing.T) {
	for name, deleted := range map[string]*DeletedPVCs{"nil record": nil, "empty record": {}} {
		t.Run(name, func(t *testing.T) {
			in := secretInputs()
			in.RecreatePVCs = true
			fake := &fakeCapsuleClient{existing: map[string]string{}, applyErrs: []error{errors.New("connection refused")}}

			rpt := runCreateCapsuleWithDeleted(t, fake, in, deleted)
			require.Equal(t, automa.StatusFailed, rpt.Status)
			assert.Empty(t, lostDataHintOf(rpt.Error))
		})
	}
}

func TestCreateConsensusCapsule_TransientApplyErrorIsRetried(t *testing.T) {
	fastCapsuleRetry(t)
	transient := errorx.InternalError.Wrap(kerrors.NewServiceUnavailable("apiserver restarting"), "failed to apply")
	fake := &fakeCapsuleClient{existing: map[string]string{}, applyErrs: []error{transient, nil}}

	rpt := runCreateCapsule(t, fake, secretInputs())
	require.Equal(t, automa.StatusSuccess, rpt.Status, rpt.Error)
	assert.Equal(t, 2, fake.applyCalls)
	assert.Len(t, fake.applied, 1)
}

func TestCreateConsensusCapsule_AdmissionRejectionIsNotRetried(t *testing.T) {
	fastCapsuleRetry(t)
	fake := &fakeCapsuleClient{existing: map[string]string{}, applyErrs: []error{errorx.RejectedOperation.New("spec.orbit must equal the target namespace")}}

	rpt := runCreateCapsule(t, fake, secretInputs())
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Equal(t, 1, fake.applyCalls)
	assert.Contains(t, rpt.Error.Error(), "spec.orbit must equal the target namespace")
}

func TestCreateConsensusCapsule_TransientErrorStopsAfterLastRetry(t *testing.T) {
	fastCapsuleRetry(t)
	fake := &fakeCapsuleClient{existing: map[string]string{}, applyErrs: []error{kerrors.NewServiceUnavailable("apiserver down")}}

	rpt := runCreateCapsule(t, fake, secretInputs())
	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Equal(t, capsuleApplyBackoff.Steps, fake.applyCalls)
	assert.Contains(t, rpt.Error.Error(), "apiserver down")
}

func runCreateCapsule(t *testing.T, fake *fakeCapsuleClient, in models.ConsensusNodeInputs) *automa.Report {
	t.Helper()
	return runCreateCapsuleWithDeleted(t, fake, in, nil)
}

func runCreateCapsuleWithDeleted(t *testing.T, fake *fakeCapsuleClient, in models.ConsensusNodeInputs, deleted *DeletedPVCs) *automa.Report {
	t.Helper()
	step, err := CreateConsensusCapsule(in, fake.provider(), deleted).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

// A failed build after the delete keeps its own reason and hints and adds the lost-data hint.
func TestCreateConsensusCapsule_BuildFailureAfterRecreateKeepsHints(t *testing.T) {
	in := secretInputs()
	in.RecreatePVCs = true
	in.CPULimit = "not-a-quantity"
	fake := &fakeCapsuleClient{existing: map[string]string{}}

	rpt := runCreateCapsuleWithDeleted(t, fake, in, deletedRecord(savedPVC))
	require.Equal(t, automa.StatusFailed, rpt.Status)
	hints := hintsOf(rpt.Error)
	assert.Equal(t, lostDataHint+savedPVC, lostDataHintOf(rpt.Error))
	assert.Contains(t, hints, "Provide valid Kubernetes quantities for --cpu-limit/--cpu-request/--memory-limit/--memory-request (e.g. 2, 250m, 5Gi, 1Gi)")
	reason, ok := errx.ReasonOf(rpt.Error)
	require.True(t, ok)
	assert.Equal(t, reasons.InvalidArgument, reason)
	assert.Empty(t, fake.applied)
}

// --- ReconcileConsensusPVCs with the capsule dry-run that it connects itself ---

func recreateInputs() models.ConsensusNodeInputs {
	in := secretInputs()
	in.Namespace, in.OrbitName, in.NodeId = "ns", "orbit", 0
	in.RecreatePVCs = true
	return in
}

func TestReconcileConsensusPVCs_RejectedDryRunDeletesNothing(t *testing.T) {
	pvc := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}}
	capsule := &fakeCapsuleClient{existing: map[string]string{}, dryRunErr: errorx.RejectedOperation.New("spec.orbit must match the namespace")}
	deleted := &DeletedPVCs{}

	step, err := ReconcileConsensusPVCs(recreateInputs(), true, pvc.provider(), capsule.provider(), deleted).Build()
	require.NoError(t, err)
	rpt := step.Execute(context.Background())

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Contains(t, rpt.Error.Error(), "spec.orbit must match the namespace")
	assert.Equal(t, []string{models.ConsensusCapsuleName("orbit", 0)}, capsule.dryRuns)
	assert.Empty(t, pvc.calls, "a rejected capsule must not delete a PVC")
	assert.Empty(t, capsule.applied)
	assert.Empty(t, deleted.Names())
}

// When the create step fails after a delete, its error and the end summary both name the PVC.
func TestConsensusInstall_SummaryNamesDeletedPVCsWhenCreateFails(t *testing.T) {
	in := recreateInputs()
	pvc := &fakePVCClient{pvcs: []unstructured.Unstructured{savedPVCObj("500Gi", "standard")}}
	capsule := &fakeCapsuleClient{existing: map[string]string{}, applyErrs: []error{errors.New("connection refused")}}

	deleted := &DeletedPVCs{}
	deploy := automa.NewWorkflowBuilder().WithId("deploy").Steps(
		ReconcileConsensusPVCs(in, true, pvc.provider(), capsule.provider(), deleted),
		CreateConsensusCapsule(in, capsule.provider(), deleted),
	)
	wf, err := automa.NewWorkflowBuilder().WithId("install").Steps(deploy).Build()
	require.NoError(t, err)
	rpt := wf.Execute(context.Background())

	require.Equal(t, automa.StatusFailed, rpt.Status)
	assert.Len(t, capsule.dryRuns, 1, "the dry-run passed before the delete")
	assert.Equal(t, []string{"PVC " + savedPVC + " (500Gi) and its data deleted (--recreate-pvcs)"}, ui.CollectWarnings(rpt))
	assert.Equal(t, lostDataHint+savedPVC, lostDataHintOf(doctor.DeepestFailureError(rpt)))
}
