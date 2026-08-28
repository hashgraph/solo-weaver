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
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	EnsureNetworkGenesisStepId   = "ensure-network-genesis"
	WaitNetworkGenesisStepId     = "wait-network-genesis"
	NetworkGenesisName           = "network-genesis"
	NetworkGenesisConfigMapName  = "genesis-config"
	networkGenesisPollInterval   = 5 * time.Second
	networkGenesisDefaultOpIDFmt = "genesis-%s"
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
