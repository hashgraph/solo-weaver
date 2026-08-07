// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"os"
	"path/filepath"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// adminKubeconfigPath is kubeadm's admin kubeconfig. Its presence is the cheapest
// reliable signal that a cluster is still provisioned on this host, and a block
// node cannot outlive the cluster it runs on — so one stat covers both.
const adminKubeconfigPath = "/etc/kubernetes/admin.conf"

// CheckNoProvisionedClusterStepId is the step ID for CheckNoProvisionedCluster.
const CheckNoProvisionedClusterStepId = "check-no-provisioned-cluster"

// CheckNoProvisionedCluster fails the self-uninstall when a Kubernetes cluster is
// still provisioned here. Removing the CLI first strands the operator: every
// teardown command lives in the binary about to be deleted, and the daemon would
// keep reconciling a block node with nothing left to manage it.
//
// This deliberately reads the host rather than the recorded state file, which can
// disagree with reality after a partial teardown.
func CheckNoProvisionedCluster() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId(CheckNoProvisionedClusterStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if _, err := os.Stat(adminKubeconfigPath); err != nil {
				return automa.StepSuccessReport(stp.Id())
			}
			return automa.StepFailureReport(stp.Id(),
				automa.WithError(errorx.IllegalState.New(
					"a Kubernetes cluster is still provisioned on this host (%s exists)", adminKubeconfigPath).
					WithProperty(models.ErrPropertyResolution, []string{
						"Tear the workloads down first, in this order:",
						"  sudo solo-provisioner block node uninstall",
						"  sudo solo-provisioner kube cluster uninstall",
						"  sudo solo-provisioner uninstall --yes",
					})))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking for provisioned workloads")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Workloads are still provisioned")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "No provisioned workloads found")
		})
}

// RemoveNetworkConfigStepId is the step ID for RemoveNetworkConfig.
const RemoveNetworkConfigStepId = "remove-network-config"

// RemoveNetworkConfig deletes the operator-facing config tree that the network
// planes persist outside the weaver home — the rendered .nft files, the policy
// registry, and the tc device/class configs.
//
// It runs before the two boot-unit teardown steps: NftServiceTeardown retains the
// shared unit while the host-firewall .nft file is still present, so removing the
// files first is what lets that step take the unit with it.
//
// Live kernel state (the loaded nft tables, the tc qdiscs) is deliberately left
// alone — this removes the boot-replay inputs, so the host comes up clean, but
// nothing here touches packet handling on a running machine.
func RemoveNetworkConfig() *automa.StepBuilder {
	// Derived rather than declared: the network packages already mirror this
	// prefix by value, and another copy would be one more thing to keep in sync.
	configDir := filepath.Dir(firewall.HostNftPath)

	return automa.NewStepBuilder().WithId(RemoveNetworkConfigStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if _, err := os.Stat(configDir); os.IsNotExist(err) {
				return automa.SkippedReport(stp,
					automa.WithDetail("network config directory already absent"))
			}
			if err := os.RemoveAll(configDir); err != nil {
				return automa.StepFailureReport(stp.Id(),
					automa.WithError(errorx.InternalError.Wrap(err,
						"failed to remove network config directory %s", configDir).
						WithProperty(models.ErrPropertyResolution, []string{
							"Remove manually: sudo rm -rf " + configDir,
						})))
			}
			logx.As().Info().Str("path", configDir).Msg("Network config directory removed")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Removing network configuration")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to remove network configuration")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Network configuration removed")
		})
}
