// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	PrecheckConsensusRunStateStepId = "precheck-consensus-run-state"
	SetConsensusRunStateStepId      = "set-consensus-run-state"
	WaitConsensusRunStateStepId     = "wait-consensus-run-state"

	// ConsensusRunStateFieldManager owns only spec.desiredRunState. It must differ
	// from install's manager, or a one-field apply would drop the rest of the spec.
	ConsensusRunStateFieldManager = "solo-weaver-runstate"
)

var consensusRunStatePollInterval = 2 * time.Second

// RunStateKubeClient is the subset of *kube.Client used by the start/stop steps.
type RunStateKubeClient interface {
	ResourceExists(ctx context.Context, apiVersion, kind, namespace, name string) (bool, error)
	GetResourceNestedMap(ctx context.Context, apiVersion, kind, namespace, name string, fields ...string) (map[string]any, bool, error)
	ApplyFieldsAs(ctx context.Context, fieldManager string, obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
}

// RunStateKubeProvider resolves a RunStateKubeClient at step-execution time.
type RunStateKubeProvider func(ctx context.Context) (RunStateKubeClient, error)

// DefaultRunStateKubeProvider returns a live kube client.
func DefaultRunStateKubeProvider(context.Context) (RunStateKubeClient, error) {
	return kube.NewClient()
}

// consensusStopIntended mirrors the operator's stopIntended rule: true when the
// node's process is meant to stay stopped.
func consensusStopIntended(startPolicy, desiredRunState string) bool {
	if operatorv1alpha1.RunState(desiredRunState) == operatorv1alpha1.RunStateStopped {
		return true
	}
	return operatorv1alpha1.StartPolicy(startPolicy) == operatorv1alpha1.StartPolicyManual &&
		operatorv1alpha1.RunState(desiredRunState) != operatorv1alpha1.RunStateRunning
}

// capsuleView holds the capsule fields the start/stop steps read.
type capsuleView struct {
	generation         int64
	observedGeneration int64
	offline            bool
	startPolicy        string
	desiredRunState    string
	phase              string
	runState           string
}

func readCapsuleView(ctx context.Context, kc RunStateKubeClient, namespace, name string) (*capsuleView, bool, error) {
	obj, found, err := kc.GetResourceNestedMap(ctx, kube.SoloOperatorAPIVersion, string(kube.KindConsensusCapsule), namespace, name)
	if err != nil || !found {
		return nil, found, err
	}
	v := &capsuleView{}
	v.generation, _, _ = unstructured.NestedInt64(obj, "metadata", "generation")
	v.observedGeneration, _, _ = unstructured.NestedInt64(obj, "status", "observedGeneration")
	v.offline, _, _ = unstructured.NestedBool(obj, "spec", "offline")
	v.startPolicy, _, _ = unstructured.NestedString(obj, "spec", "startPolicy")
	v.desiredRunState, _, _ = unstructured.NestedString(obj, "spec", "desiredRunState")
	v.phase, _, _ = unstructured.NestedString(obj, "status", "phase")
	v.runState, _, _ = unstructured.NestedString(obj, "status", "runState")
	if v.phase == "" {
		v.phase = string(operatorv1alpha1.PhasePending)
	}
	return v, true, nil
}

func runStateVerb(desired operatorv1alpha1.RunState) string {
	if desired == operatorv1alpha1.RunStateRunning {
		return "start"
	}
	return "stop"
}

func runStateDone(desired operatorv1alpha1.RunState) string {
	if desired == operatorv1alpha1.RunStateRunning {
		return "started"
	}
	return "stopped"
}

func runStateConnectFailure(stp automa.Step, err error) *automa.Report {
	return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
		errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
		reasons.PreconditionNotMet,
		"Verify your kubeconfig and that 'kubectl get nodes' works")))
}

func runStateReadFailure(stp automa.Step, err error, namespace, name string) *automa.Report {
	return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
		errorx.IllegalState.Wrap(err, "failed to read ConsensusCapsule %s", name),
		reasons.K8sAPIFailed,
		"Verify cluster connectivity and that your kubeconfig can read solo-operator CRs",
		fmt.Sprintf("Check the capsule: kubectl -n %s get consensuscapsule %s", namespace, name))))
}

// PrecheckConsensusRunState refuses start/stop on a node that is not installed,
// and refuses start on a node that is offline or has no network genesis yet.
func PrecheckConsensusRunState(inputs models.ConsensusNodeInputs, desired operatorv1alpha1.RunState, provider RunStateKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(PrecheckConsensusRunStateStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return runStateConnectFailure(stp, err)
			}
			name := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)

			v, found, err := readCapsuleView(ctx, kc, inputs.Namespace, name)
			if err != nil {
				return runStateReadFailure(stp, err, inputs.Namespace, name)
			}
			if !found {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.New("consensus node %d is not installed in namespace %s (ConsensusCapsule %s not found)", inputs.NodeId, inputs.Namespace, name),
					reasons.NotInstalled,
					fmt.Sprintf("List the installed nodes: kubectl -n %s get consensuscapsules", inputs.Namespace),
					fmt.Sprintf("Install the node first: sudo solo-provisioner consensus node install --experimental --namespace %s --node-id %d", inputs.Namespace, inputs.NodeId))))
			}

			if desired == operatorv1alpha1.RunStateRunning {
				if v.offline {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.New("consensus node %d is offline (spec.offline=true), so it has no pod to start", inputs.NodeId),
						reasons.PreconditionNotMet,
						fmt.Sprintf("Set spec.offline to false first: kubectl -n %s patch consensuscapsule %s --type merge -p '{\"spec\":{\"offline\":false}}'", inputs.Namespace, name))))
				}
				genesis, err := kc.ResourceExists(ctx, "v1", "ConfigMap", inputs.Namespace, NetworkGenesisConfigMapName)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(err, "failed to read ConfigMap %s", NetworkGenesisConfigMapName),
						reasons.K8sAPIFailed,
						"Verify cluster connectivity and that your kubeconfig can read ConfigMaps",
						fmt.Sprintf("Check the network genesis: kubectl -n %s get configmap %s", inputs.Namespace, NetworkGenesisConfigMapName))))
				}
				if !genesis {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.New("namespace %s has no network genesis yet, so consensus node %d cannot start", inputs.Namespace, inputs.NodeId),
						reasons.PreconditionNotMet,
						fmt.Sprintf("Generate the network genesis first: sudo solo-provisioner consensus network genesis --experimental --namespace %s", inputs.Namespace))))
				}
			}

			notify.As().StepDetail(ctx, stp, fmt.Sprintf("node %d: phase=%s runState=%s desiredRunState=%s",
				inputs.NodeId, v.phase, valueOrDefault(v.runState, "unknown"), valueOrDefault(v.desiredRunState, "unset")))
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Checking that consensus node %d can %s", inputs.NodeId, runStateVerb(desired)))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, fmt.Sprintf("Consensus node %d cannot %s", inputs.NodeId, runStateVerb(desired)))
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Consensus node precheck passed")
		})
}

// SetConsensusRunState server-side applies only spec.desiredRunState on the
// node's capsule; re-applying the same value changes nothing.
func SetConsensusRunState(inputs models.ConsensusNodeInputs, desired operatorv1alpha1.RunState, provider RunStateKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(SetConsensusRunStateStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return runStateConnectFailure(stp, err)
			}
			name := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)

			if _, err := kc.ApplyFieldsAs(ctx, ConsensusRunStateFieldManager, desiredRunStatePatch(inputs.Namespace, name, desired)); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to set desiredRunState=%s on ConsensusCapsule %s", desired, name),
					reasons.K8sAPIFailed,
					"Verify cluster connectivity and that your kubeconfig has RBAC to patch solo-operator ConsensusCapsule CRs",
					fmt.Sprintf("Check the capsule: kubectl -n %s get consensuscapsule %s -o yaml", inputs.Namespace, name))))
			}

			logx.As().Info().Str("name", name).Str("desiredRunState", string(desired)).Msg("Set ConsensusCapsule desired run state")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				"desiredRunState": string(desired),
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Setting desiredRunState=%s on consensus node %d", desired, inputs.NodeId))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to set the desired run state")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, fmt.Sprintf("desiredRunState=%s set", desired))
		})
}

// desiredRunStatePatch is the whole apply body: identity plus the one owned field.
func desiredRunStatePatch(namespace, name string, desired operatorv1alpha1.RunState) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": kube.SoloOperatorAPIVersion,
		"kind":       string(kube.KindConsensusCapsule),
		"metadata": map[string]any{
			"name":      name,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"desiredRunState": string(desired),
		},
	}}
}

// runStateTimeout is the wait's timeout report. last is the latest read of the
// current generation, or nil; readErr is the error of the latest read, or nil.
func runStateTimeout(stp automa.Step, inputs models.ConsensusNodeInputs, desired operatorv1alpha1.RunState, timeout time.Duration, last *capsuleView, readErr error) *automa.Report {
	ns := inputs.Namespace
	name := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)
	hints := []string{
		fmt.Sprintf("Check node status: kubectl -n %s get consensuscapsule %s -o yaml", ns, name),
		fmt.Sprintf("Inspect the pod: kubectl -n %s get pods; kubectl -n %s describe pod <pod>", ns, ns),
		"desiredRunState is set; the operator keeps acting on it. Re-run with a larger --ready-timeout to wait longer",
	}
	if desired == operatorv1alpha1.RunStateStopped && last != nil &&
		operatorv1alpha1.RunState(last.runState) == operatorv1alpha1.RunStateStopped {
		hints = append(hints,
			fmt.Sprintf("The operator reports runState=Stopped but phase=%s: the consensus process can still run (https://github.com/hashgraph/solo-weaver/issues/1479)", last.phase),
			fmt.Sprintf("Check the platform status: kubectl -n %s get consensuscapsule %s -o jsonpath='{.status.phase} {.status.platformStatus}'", ns, name))
	}
	if readErr != nil {
		hints = append([]string{"Verify cluster connectivity and that your kubeconfig can read solo-operator CRs"}, hints...)
		return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
			errorx.IllegalState.Wrap(readErr, "timed out after %s waiting for consensus node %d to %s; the last status read failed", timeout, inputs.NodeId, runStateVerb(desired)),
			reasons.K8sAPIFailed,
			hints...)))
	}
	return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
		errorx.IllegalState.New("timed out after %s waiting for consensus node %d to %s", timeout, inputs.NodeId, runStateVerb(desired)),
		reasons.PreconditionNotMet,
		hints...)))
}

// WaitConsensusRunState waits for phase Running/Active (start) or for
// runState=Stopped and phase Stopped (stop), then reports. A timeout of 0
// reports without a wait.
func WaitConsensusRunState(inputs models.ConsensusNodeInputs, desired operatorv1alpha1.RunState, timeout time.Duration, provider RunStateKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(WaitConsensusRunStateStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return runStateConnectFailure(stp, err)
			}
			ns := inputs.Namespace
			name := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)

			success := func(v *capsuleView, note string) *automa.Report {
				msg := fmt.Sprintf("node %d: phase=%s runState=%s", inputs.NodeId, v.phase, valueOrDefault(v.runState, "unknown"))
				if note != "" {
					msg += " (" + note + ")"
				}
				notify.As().StepDetail(ctx, stp, msg)
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
					"phase":    v.phase,
					"runState": v.runState,
				}))
			}

			// The generation that holds our change; older status is stale.
			var target int64
			var last *capsuleView // last current-generation read that was not done
			var lastReadErr error // error of the latest read, or nil
			genesisSeen := false  // genesis does not go away, so stop reads it until seen
			checkOnce := func() (bool, *automa.Report) {
				v, found, err := readCapsuleView(ctx, kc, ns, name)
				if err != nil {
					lastReadErr = err
					notify.As().StepDetail(ctx, stp, fmt.Sprintf("node %d: status unavailable (%v)", inputs.NodeId, err))
					return false, nil
				}
				lastReadErr = nil
				if !found {
					return true, automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.New("ConsensusCapsule %s was deleted while waiting", name),
						reasons.NotInstalled,
						fmt.Sprintf("List the installed nodes: kubectl -n %s get consensuscapsules", ns))))
				}
				if target == 0 {
					target = v.generation
				}
				if timeout == 0 {
					return true, success(v, "not waited: --ready-timeout is 0")
				}

				// Stop on an offline node or before genesis: no process runs, so
				// the operator cannot report runState. The node stays stopped.
				if desired == operatorv1alpha1.RunStateStopped {
					if v.offline {
						return true, success(v, "node is offline; it stays stopped when brought back")
					}
					if !genesisSeen {
						genesis, gerr := kc.ResourceExists(ctx, "v1", "ConfigMap", ns, NetworkGenesisConfigMapName)
						if gerr == nil && !genesis {
							return true, success(v, "no network genesis yet; the node stays stopped after genesis")
						}
						genesisSeen = gerr == nil
					}
				}

				if v.observedGeneration < target {
					notify.As().StepDetail(ctx, stp, fmt.Sprintf("node %d: waiting for the operator to observe the change", inputs.NodeId))
					return false, nil
				}
				if operatorv1alpha1.Phase(v.phase) == operatorv1alpha1.PhaseFailed {
					return true, automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.New("consensus node %d reached phase Failed", inputs.NodeId),
						reasons.PreconditionNotMet,
						fmt.Sprintf("Inspect the node: kubectl -n %s describe consensuscapsule %s", ns, name),
						"Check the solo-operator logs and the consensus node pod logs in that namespace")))
				}

				switch desired {
				case operatorv1alpha1.RunStateRunning:
					p := operatorv1alpha1.Phase(v.phase)
					if p == operatorv1alpha1.PhaseRunning || p == operatorv1alpha1.PhaseActive {
						return true, success(v, "")
					}
				case operatorv1alpha1.RunStateStopped:
					// runState only means s6 is down; phase Stopped also needs the platform silent (#1479).
					if operatorv1alpha1.RunState(v.runState) == operatorv1alpha1.RunStateStopped &&
						operatorv1alpha1.Phase(v.phase) == operatorv1alpha1.PhaseStopped {
						return true, success(v, "")
					}
				}
				last = v
				notify.As().StepDetail(ctx, stp, fmt.Sprintf("node %d: phase=%s runState=%s", inputs.NodeId, v.phase, valueOrDefault(v.runState, "unknown")))
				return false, nil
			}

			if done, rpt := checkOnce(); done {
				return rpt
			}
			deadline := time.Now().Add(timeout)
			ticker := time.NewTicker(consensusRunStatePollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(ctx.Err(), "cancelled while waiting for consensus node %d to %s", inputs.NodeId, runStateVerb(desired)),
						reasons.PreconditionNotMet,
						fmt.Sprintf("Check node status: kubectl -n %s get consensuscapsule %s", ns, name))))
				case <-ticker.C:
					if done, rpt := checkOnce(); done {
						return rpt
					}
					if time.Now().After(deadline) {
						return runStateTimeout(stp, inputs, desired, timeout, last, lastReadErr)
					}
				}
			}
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Waiting for consensus node %d to %s (timeout %s)", inputs.NodeId, runStateVerb(desired), timeout))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, fmt.Sprintf("Consensus node %d did not %s", inputs.NodeId, runStateVerb(desired)))
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, fmt.Sprintf("Consensus node %d %s", inputs.NodeId, runStateDone(desired)))
		})
}
