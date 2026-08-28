// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	EnsureNetworkGenesisStepId      = "ensure-network-genesis"
	WaitNetworkGenesisStepId        = "wait-network-genesis"
	WaitConsensusNetworkReadyStepId = "wait-consensus-network-ready"
	NetworkGenesisName              = "network-genesis"
	NetworkGenesisConfigMapName     = "genesis-config"
	networkGenesisPollInterval      = 5 * time.Second
	networkGenesisDefaultOpIDFmt    = "genesis-%s"
)

// EnsureNetworkGenesis creates the orbit's NetworkGenesis CR so the operator
// generates genesis-network.json. When genesisNetworkJSON is non-empty (a
// pre-built genesis from the deployment package) it is written verbatim,
// skipping the operator's roster discovery; when empty the operator discovers
// the roster from the ConsensusCapsules in the namespace.
func EnsureNetworkGenesis(namespace, orbit, genesisNetworkJSON string, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(EnsureNetworkGenesisStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			ng := &operatorv1alpha1.NetworkGenesis{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion,
					Kind:       string(kube.KindNetworkGenesis),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NetworkGenesisName,
					Namespace: namespace,
				},
				Spec: operatorv1alpha1.NetworkGenesisSpec{
					Orbit:              orbit,
					OperationID:        fmt.Sprintf(networkGenesisDefaultOpIDFmt, orbit),
					GenesisNetworkJSON: genesisNetworkJSON,
				},
			}

			if err := kc.ApplyTyped(ctx, ng); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "failed to apply NetworkGenesis for orbit %s", orbit),
					reasons.PreconditionNotMet,
					"Verify cluster connectivity and that your kubeconfig has RBAC to create solo-operator NetworkGenesis CRs")))
			}

			source := "cluster discovery"
			if genesisNetworkJSON != "" {
				source = "deployment package (pre-built genesis-network.json)"
			}
			logx.As().Info().Str("orbit", orbit).Str("source", source).Msg("NetworkGenesis applied")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				InstalledByThisStep: "true",
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Creating NetworkGenesis for orbit %s", orbit))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create NetworkGenesis")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "NetworkGenesis created")
		})
}

// WaitConsensusNetworkReady polls every ConsensusCapsule in the namespace until
// they all reach Running/Active — the confirmation that, after genesis unblocked
// the network, the consensus nodes actually came up. It fails fast if any capsule
// reports Failed and times out otherwise. Live per-node phases are surfaced via
// StepDetail so the operator can watch the network converge (and see a stuck node,
// e.g. an ImagePullBackOff, as a timeout rather than a silent hang). It is a
// namespace-wide companion to WaitConsensusCapsuleReady (which is per-node during
// install) and needs no node IDs — it discovers the capsules from the cluster.
func WaitConsensusNetworkReady(namespace string, provider CapsuleKubeProvider, timeout time.Duration) automa.Builder {
	return automa.NewStepBuilder().WithId(WaitConsensusNetworkReadyStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			deadline := time.Now().Add(timeout)
			ticker := time.NewTicker(networkGenesisPollInterval)
			defer ticker.Stop()

			checkOnce := func() (done bool, rpt *automa.Report) {
				list, err := kc.List(ctx, kube.KindConsensusCapsule, namespace, kube.WaitOptions{})
				if err != nil {
					notify.As().StepDetail(ctx, stp, "waiting for consensus capsules…")
					return false, nil
				}
				if list == nil || len(list.Items) == 0 {
					notify.As().StepDetail(ctx, stp, "no ConsensusCapsules found in namespace yet…")
					return false, nil
				}

				// Only Auto-start nodes are expected to come up on their own after
				// genesis. Manual-start nodes stay in Stopped until `consensus node
				// start`, so they are excluded from the wait (reported, not blocked).
				autoTotal := 0
				ready := 0
				manual := 0
				var failed []string
				details := make([]string, 0, len(list.Items))
				for i := range list.Items {
					item := &list.Items[i]
					name := item.GetName()
					phase, _, _ := unstructured.NestedString(item.Object, "status", "phase")
					if phase == "" {
						phase = string(operatorv1alpha1.PhasePending)
					}
					policy, _, _ := unstructured.NestedString(item.Object, "spec", "startPolicy")
					if policy == "" {
						policy = string(operatorv1alpha1.StartPolicyAuto) // CRD default is Auto
					}

					if operatorv1alpha1.StartPolicy(policy) == operatorv1alpha1.StartPolicyManual {
						manual++
						details = append(details, fmt.Sprintf("%s=%s(manual)", name, phase))
						continue
					}

					autoTotal++
					details = append(details, fmt.Sprintf("%s=%s", name, phase))
					switch operatorv1alpha1.Phase(phase) {
					case operatorv1alpha1.PhaseRunning, operatorv1alpha1.PhaseActive:
						ready++
					case operatorv1alpha1.PhaseFailed:
						failed = append(failed, name)
					}
				}
				sort.Strings(details)
				notify.As().StepDetail(ctx, stp, fmt.Sprintf("auto nodes ready %d/%d (manual %d) — %s", ready, autoTotal, manual, strings.Join(details, " ")))

				if len(failed) > 0 {
					sort.Strings(failed)
					return true, automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.New("consensus capsule(s) %s reached phase Failed", strings.Join(failed, ", ")),
						reasons.PreconditionNotMet,
						fmt.Sprintf("Inspect the failed node(s): kubectl -n %s describe consensuscapsule %s", namespace, failed[0]),
						"Check the solo-operator logs and the consensus node pod logs in that namespace")))
				}
				// Ready when every Auto node is up. All-Manual (autoTotal==0) settles
				// immediately — nothing auto-starts, so there is nothing to wait for.
				if ready == autoTotal {
					return true, automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
						"nodesReady":  fmt.Sprintf("%d", ready),
						"manualNodes": fmt.Sprintf("%d", manual),
					}))
				}
				return false, nil
			}

			if done, rpt := checkOnce(); done {
				return rpt
			}
			for {
				select {
				case <-ctx.Done():
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(ctx.Err(), "cancelled while waiting for consensus nodes to become ready"),
						reasons.PreconditionNotMet,
						fmt.Sprintf("Check node status: kubectl -n %s get consensuscapsules", namespace))))
				case <-ticker.C:
					if done, rpt := checkOnce(); done {
						return rpt
					}
					if time.Now().After(deadline) {
						return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
							errorx.IllegalState.New("timed out after %s waiting for all consensus nodes to become Running/Active", timeout),
							reasons.PreconditionNotMet,
							fmt.Sprintf("Check node status: kubectl -n %s get consensuscapsules", namespace),
							fmt.Sprintf("Inspect a stuck node's pod: kubectl -n %s get pods; kubectl -n %s describe pod <pod>", namespace, namespace),
							"A node stuck pulling images needs a registry pull secret; increase --ready-timeout if it just needs more time (image pull, event replay)",
							"Only Auto-start nodes are awaited; a Manual-start node stays Stopped until 'consensus node start'")))
					}
				}
			}
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Waiting for consensus nodes to be running (timeout %s)", timeout))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Consensus nodes did not all become ready")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "All consensus nodes are running")
		})
}

// WaitNetworkGenesisReady polls until the operator has produced the genesis
// ConfigMap (genesis-config) for the orbit — the signal that genesis-network.json
// is generated and the consensus nodes' genesis-init containers can proceed.
func WaitNetworkGenesisReady(namespace string, provider CapsuleKubeProvider, timeout time.Duration) automa.Builder {
	return automa.NewStepBuilder().WithId(WaitNetworkGenesisStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			deadline := time.Now().Add(timeout)
			ticker := time.NewTicker(networkGenesisPollInterval)
			defer ticker.Stop()

			check := func() (bool, error) {
				return kc.ResourceExists(ctx, "v1", "ConfigMap", namespace, NetworkGenesisConfigMapName)
			}

			if ok, _ := check(); ok {
				return automa.StepSuccessReport(stp.Id())
			}
			for {
				select {
				case <-ctx.Done():
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(ctx.Err(), "cancelled while waiting for genesis ConfigMap %q", NetworkGenesisConfigMapName),
						reasons.PreconditionNotMet,
						fmt.Sprintf("Check the NetworkGenesis status: kubectl -n %s get networkgenesis %s -o yaml", namespace, NetworkGenesisName))))
				case <-ticker.C:
					if ok, _ := check(); ok {
						return automa.StepSuccessReport(stp.Id())
					}
					notify.As().StepDetail(ctx, stp, "waiting for the operator to generate genesis-network.json…")
					if time.Now().After(deadline) {
						return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
							errorx.IllegalState.New("timed out after %s waiting for genesis ConfigMap %q", timeout, NetworkGenesisConfigMapName),
							reasons.PreconditionNotMet,
							fmt.Sprintf("Check the NetworkGenesis status: kubectl -n %s get networkgenesis %s -o yaml", namespace, NetworkGenesisName),
							"Confirm at least one ConsensusCapsule exists in the namespace (or a genesis was provided) and the solo-operator is running")))
					}
				}
			}
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Waiting for network genesis (timeout %s)", timeout))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Network genesis not generated")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Network genesis generated")
		})
}
