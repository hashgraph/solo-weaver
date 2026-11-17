// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall the External Secrets Operator",
	Long: `Uninstall the External Secrets Operator (ESO) Helm release from the cluster.

The target is the ESO release in --namespace, located by chart name: this clears
an ESO installed under a different release name, and one left in a failed,
pending, uninstalling or uninstalled state. It never reaches outside that
namespace.

The command is idempotent: when the namespace holds no ESO, it exits cleanly with
a skip message. A different chart holding the "external-secrets" release name is
left untouched, with a warning naming it.

The uninstall refuses, before changing anything, while ExternalSecrets,
ClusterExternalSecrets, GeneratorStates or PushSecrets with deletionPolicy Delete
exist anywhere in the cluster. ESO puts a finalizer on each that only its
controller clears, and the chart removes the controller and the CRDs together,
so uninstalling over them would leave the CRDs stuck deleting. Delete them first
(eso secret delete for ExternalSecrets). --force and --non-interactive do not
skip this check. A release left failed, pending or uninstalling whose objects are
all already being deleted is uninstalled anyway; recover with
install --recover-partial-uninstall.

When it runs, the uninstall removes ESO's cluster-scoped CRDs, which deletes every
SecretStore and ClusterSecretStore in the cluster.

Examples:
  # Uninstall ESO from the default namespace (external-secrets)
  solo-provisioner eso operator uninstall

  # Uninstall from a custom namespace
  solo-provisioner eso operator uninstall --namespace my-eso`,
	RunE: func(cmd *cobra.Command, args []string) error {
		l := logx.As()
		l.Debug().
			Strs("args", args).
			Str("namespace", flagESONamespace).
			Msg("Uninstalling External Secrets Operator")

		// Argument order is (continue, stop, rollback).
		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return err
		}
		opts := workflows.DefaultWorkflowExecutionOptions()
		opts.ExecutionMode = execMode

		wb := workflows.WithWorkflowExecutionMode(workflows.NewESOUninstallWorkflow(flagESONamespace), opts)

		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		l.Info().Msg("Successfully uninstalled External Secrets Operator")
		return nil
	},
}
