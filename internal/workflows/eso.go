// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

// ESOInstallOptions aliases the steps-layer options so cmd callers need not
// import internal/workflows/steps.
type ESOInstallOptions = steps.ESOInstallOptions

// NewESOInstallWorkflow creates a workflow to install the External Secrets
// Operator (ESO) Helm chart into the cluster. It returns an error when the
// requested version is not declared in the infrastructure catalog.
func NewESOInstallWorkflow(opts ESOInstallOptions) (*automa.WorkflowBuilder, error) {
	return steps.SetupExternalSecrets(opts)
}
