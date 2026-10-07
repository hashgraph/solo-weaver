// SPDX-License-Identifier: Apache-2.0

package models

import "time"

// ConsensusNetworkInputs carries the user inputs for orbit/network-level
// consensus operations (currently `consensus network genesis`). Namespace
// doubles as the Orbit name by convention; GenesisFile / DeploymentPackageDir
// are resolved into GenesisNetworkJSON during effective-input preparation.
type ConsensusNetworkInputs struct {
	// Namespace is the Kubernetes namespace / Orbit name of the network.
	Namespace string
	// Orbit is the resolved orbit name (defaults to Namespace).
	Orbit string

	// GenesisFile, when set, is a genesis-network.json applied verbatim; it
	// overrides the deployment package's genesis-network.json when both are set.
	GenesisFile string
	// DeploymentPackageDir, when set, points at an extracted HIP-1494 deployment
	// package whose genesis-network.json is applied as the pre-built genesis.
	DeploymentPackageDir string
	// GenesisNetworkJSON is the resolved pre-built genesis contents (from
	// GenesisFile or DeploymentPackageDir); empty means discover the roster from
	// the ConsensusCapsules in the namespace.
	GenesisNetworkJSON string

	// ReadyTimeout is how long to wait for the operator to generate the genesis
	// ConfigMap and for the consensus nodes to become Active (0 disables the wait).
	ReadyTimeout time.Duration
}
