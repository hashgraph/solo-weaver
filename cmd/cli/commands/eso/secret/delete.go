// SPDX-License-Identifier: Apache-2.0

package secret

import (
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/sanity"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete an ExternalSecret",
	Long: `Delete an ExternalSecret resource from the cluster.

For ExternalSecrets created by "eso secret create", the Kubernetes Secret goes
with it: that manifest sets creationPolicy: Owner, so Kubernetes garbage-collects
the Secret once its owner is gone. Collection is asynchronous, so the Secret may
outlive the command by a moment. An ExternalSecret created outside this CLI
without that policy is still deleted, but its Secret is left behind.

The command is idempotent: deleting an ExternalSecret that does not exist is
reported as skipped and exits 0. It does fail, however, once the External Secrets
Operator has been uninstalled, because ExternalSecret is then not a kind the
cluster recognises — delete your secrets before removing the operator.

Example:
  solo-provisioner eso secret delete \
    --name=grafana-alloy-secrets \
    --namespace=grafana-alloy`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateDeleteFlags(flagSecretName, flagSecretNamespace); err != nil {
			return err
		}

		l := logx.As()
		l.Debug().
			Strs("args", args).
			Str("name", flagSecretName).
			Str("namespace", flagSecretNamespace).
			Msg("Deleting ExternalSecret")

		wb := workflows.NewESOSecretDeleteWorkflow(flagSecretName, flagSecretNamespace)

		if err := common.RunWorkflowBuilder(cmd.Context(), wb); err != nil {
			return err
		}

		// Deliberately does not claim a deletion: the workflow reports Skipped when the
		// ExternalSecret was already absent, and RunWorkflowBuilder returns only an error.
		l.Info().Msg("Completed eso secret delete")
		return nil
	},
}

// validateDeleteFlags checks the ExternalSecret coordinates before any cluster
// call. Extracted from RunE so it is reachable from a unit test.
func validateDeleteFlags(name, namespace string) error {
	if err := sanity.ValidateIdentifier(name); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --name")
	}
	if err := sanity.ValidateIdentifier(namespace); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid --namespace")
	}
	return nil
}

func init() {
	common.FlagESOSecretName().SetVar(deleteCmd, &flagSecretName, true)
	common.FlagESOSecretNamespace().SetVar(deleteCmd, &flagSecretNamespace, true)
}
