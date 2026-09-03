// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/workflows/steps"
)

// NewESOInstallWorkflow creates a workflow to install the External Secrets
// Operator (ESO) Helm chart at the catalog default version. An empty namespace
// selects the catalog default.
func NewESOInstallWorkflow(namespace string) *automa.WorkflowBuilder {
	return steps.SetupExternalSecrets(namespace)
}

// NewESOUninstallWorkflow creates a workflow to uninstall the External Secrets
// Operator (ESO) Helm release. An empty namespace selects the catalog default.
func NewESOUninstallWorkflow(namespace string) *automa.WorkflowBuilder {
	return steps.TeardownExternalSecrets(namespace)
}

// ESOSecretOptions aliases the steps-layer options so cmd callers need not
// import internal/workflows/steps.
type ESOSecretOptions = steps.ESOSecretOptions

// NewESOSecretCreateWorkflow creates a workflow that applies an ExternalSecret to
// the cluster, which ESO reconciles into the target Kubernetes Secret.
func NewESOSecretCreateWorkflow(opts ESOSecretOptions) *automa.WorkflowBuilder {
	return steps.SetupESOSecret(opts)
}

// NewESOSecretDeleteWorkflow creates a workflow that deletes an ExternalSecret
// from the cluster. Kubernetes garbage-collects the Secret it synced.
func NewESOSecretDeleteWorkflow(name, namespace string) *automa.WorkflowBuilder {
	return steps.TeardownESOSecret(name, namespace)
}
