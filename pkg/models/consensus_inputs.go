// SPDX-License-Identifier: Apache-2.0

package models

// ConsensusNodeInputs holds user-supplied values for deploying a consensus node
// via the solo-operator's ConsensusCapsule CRD.
type ConsensusNodeInputs struct {
	// Namespace for the ConsensusCapsule CR
	Namespace string `json:"namespace"`

	// OrbitName is the name of the Orbit CR to associate with
	OrbitName string `json:"orbitName"`

	// NodeId is the consensus node ID (0-based)
	NodeId int64 `json:"nodeId"`

	// AccountId is the node's account ID (e.g. "0.0.3")
	AccountId string `json:"accountId"`

	// Weight is the consensus weight for this node
	Weight int `json:"weight"`

	// LedgerId is the hex ledger identity (e.g. "0x00" for mainnet)
	LedgerId string `json:"ledgerId"`

	// ChainId is the decimal EVM chain ID (e.g. "295")
	ChainId string `json:"chainId,omitempty"`

	// ConsensusImageRepo is the container image repository (e.g. "ghcr.io/hiero-ledger/hiero-consensus-node")
	ConsensusImageRepo string `json:"consensusImageRepo"`

	// ConsensusImageTag is the container image tag (e.g. "v0.58.0")
	ConsensusImageTag string `json:"consensusImageTag"`

	// WithProxy optionally creates a proxy CR alongside the node ("haproxy" or "envoy")
	WithProxy string `json:"withProxy,omitempty"`

	// Log4j2ConfigFile overrides the default log4j2.xml content from a file
	Log4j2ConfigFile string `json:"log4j2ConfigFile,omitempty"`

	// SettingsFile overrides the default settings.txt content from a file
	SettingsFile string `json:"settingsFile,omitempty"`

	// ApplicationPropertiesFile overrides the default application.properties content from a file
	ApplicationPropertiesFile string `json:"applicationPropertiesFile,omitempty"`

	// DeploymentPackageDir points to the extracted build zip (HIP-1494 structure)
	DeploymentPackageDir string `json:"deploymentPackageDir,omitempty"`

	// Config override flags (individual flag > deployment package > embedded default)
	ApiPermissionFile      string `json:"apiPermissionFile,omitempty"`
	AppOverrideFile        string `json:"appOverrideFile,omitempty"`
	BootstrapFile          string `json:"bootstrapFile,omitempty"`
	NodePropertiesFile     string `json:"nodePropertiesFile,omitempty"`
	FeeSchedulesFile       string `json:"feeSchedulesFile,omitempty"`
	SimpleFeesSchedulesFile string `json:"simpleFeesSchedulesFile,omitempty"`
	ThrottlesFile          string `json:"throttlesFile,omitempty"`
	BlockNodesConfigFile   string `json:"blockNodesConfigFile,omitempty"`

	// GrpcTlsSecret is the name of the K8s Secret containing gRPC TLS key/cert
	GrpcTlsSecret string `json:"grpcTlsSecret,omitempty"`

	// SigningSecret is the name of the K8s Secret containing gossip signing key/cert
	SigningSecret string `json:"signingSecret,omitempty"`

	// HapiAppSecret is the name of the K8s Secret containing hedera.crt and hedera.key
	HapiAppSecret string `json:"hapiAppSecret,omitempty"`
}
