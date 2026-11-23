// SPDX-License-Identifier: Apache-2.0

package operator

import (
	"github.com/automa-saga/logx"
	"github.com/spf13/cobra"

	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/workflows"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

var flagRecoverPartialUninstall bool

func init() {
	common.FlagRecoverPartialUninstall().SetVar(installCmd, &flagRecoverPartialUninstall, false)
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the External Secrets Operator",
	Long: `Install the External Secrets Operator (ESO) Helm chart into the cluster.

The command is idempotent: when ESO is already installed in the target
namespace, installation is skipped with a clear message. The chart version is
pinned by the infrastructure catalog.

--recover-partial-uninstall reinstalls over a partial uninstall: one that
removed the Helm release but left ESO's CRDs being deleted. It is refused while
any Secret is still owned by an ExternalSecret or any PushSecret has
deletionPolicy Delete. It waits for the old CRDs to finish deleting, then
uninstalls and installs ESO again to restore every CRD; recreate the stores and
ExternalSecrets afterwards. See docs/commands/alloy.md, "Recover from a partial
uninstall".

Examples:
  # Install ESO with defaults (namespace: external-secrets)
  solo-provisioner eso operator install

  # Install into a custom namespace
  solo-provisioner eso operator install --namespace my-eso

  # Reinstall over a partial uninstall, after detaching the Secrets to keep
  solo-provisioner eso operator install --recover-partial-uninstall`,
	RunE: func(cmd *cobra.Command, args []string) error {
		l := logx.As()
		l.Debug().
			Strs("args", args).
			Str("namespace", flagESONamespace).
			Msg("Installing External Secrets Operator")

		// Argument order is (continue, stop, rollback).
		execMode, err := common.GetExecutionMode(flagContinueOnError, flagStopOnError, flagRollbackOnError)
		if err != nil {
			return err
		}
		opts := workflows.DefaultWorkflowExecutionOptions()
		opts.ExecutionMode = execMode

		wb := workflows.WithWorkflowExecutionMode(workflows.NewESOInstallWorkflow(flagESONamespace, flagRecoverPartialUninstall), opts)

		intent := models.Intent{Action: models.ActionInstall, Target: models.TargetESO}
		inputs := map[string]any{"namespace": flagESONamespace}
		if err := common.RunWorkflowRecordingCluster(cmd.Context(), wb, intent, inputs); err != nil {
			return err
		}

		l.Info().Msg("Successfully installed External Secrets Operator")
		return nil
	},
}
