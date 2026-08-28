// SPDX-License-Identifier: Apache-2.0

package models

import (
	"fmt"
	"time"

	"github.com/joomcode/errorx"
)

// ConsensusNodeInputs holds user-supplied values for deploying a consensus node
// via the solo-operator's ConsensusCapsule CRD.
type ConsensusNodeInputs struct {
	Namespace string `json:"namespace"`
	OrbitName string `json:"orbitName"`
	NodeId    int64  `json:"nodeId"`
	AccountId string `json:"accountId"`
	Weight    int    `json:"weight"`

	LedgerId string `json:"ledgerId"`
	ChainId  string `json:"chainId,omitempty"`

	ConsensusImageRepo string `json:"consensusImageRepo"`
	ConsensusImageTag  string `json:"consensusImageTag"`

	DeploymentPackageDir string `json:"deploymentPackageDir,omitempty"`

	GrpcTlsSecret string `json:"grpcTlsSecret,omitempty"`
	SigningSecret string `json:"signingSecret,omitempty"`

	UpgradeOperator bool `json:"upgradeOperator,omitempty"`

	// Profile is the deployment profile (local/testnet/mainnet/...) used to size
	// the host hardware floor when this install bootstraps the cluster.
	Profile string `json:"profile,omitempty"`

	// SkipHardwareChecks bypasses the preflight hardware floor during cluster bootstrap.
	SkipHardwareChecks bool `json:"-"`

	// ReadyTimeout bounds how long install waits for the ConsensusCapsule to reach
	// Running/Active after the CR is created. Zero disables the readiness wait.
	ReadyTimeout time.Duration `json:"-"`

	// Resolved config file contents (populated by BLL, not by CLI flags)
	ConfigLog4j2                string `json:"-"`
	ConfigSettings              string `json:"-"`
	ConfigAppProperties         string `json:"-"`
	ConfigAppOverrideProperties string `json:"-"`
	ConfigApiPermission         string `json:"-"`
	ConfigBootstrap             string `json:"-"`
	ConfigNodeProperties        string `json:"-"`
	ConfigFeeSchedules          string `json:"-"`
	ConfigSimpleFeesSchedules   string `json:"-"`
	ConfigThrottles             string `json:"-"`
	ConfigBlockNodes            string `json:"-"`

	// ConfigSources records, per config key (ConfigKey*), whether the resolved
	// content came from the deployment package or the embedded default. Populated
	// by the BLL during resolution; drives the apply policy for config CRs.
	ConfigSources map[string]string `json:"-"`
}

// Config keys identify each consensus config file across resolution, hashing,
// and CR application. They double as the ConfigSources / ConfigHashes map keys.
const (
	ConfigKeyLog4j2              = "log4j2"
	ConfigKeySettings            = "settings"
	ConfigKeyAppProperties       = "application-properties"
	ConfigKeyAppOverride         = "application-override-properties"
	ConfigKeyApiPermission       = "api-permission-properties"
	ConfigKeyBootstrap           = "bootstrap-properties"
	ConfigKeyNodeProperties      = "node-properties"
	ConfigKeyFeeSchedules        = "fee-schedules"
	ConfigKeySimpleFeesSchedules = "simple-fees-schedules"
	ConfigKeyThrottles           = "throttles"
	ConfigKeyBlockNodes          = "block-nodes"
)

// consensusConfigCRSuffix maps each config key to the canonical name suffix the
// solo-operator expects. The config CR, the ConfigMap the operator derives from
// it, and the ConsensusCapsule *Ref that points at it all share this exact name
// (verified against the solo-operator docs/example). This is the single source
// of truth so EnsureConfigCRs and the capsule refs can never drift apart — a
// mismatch leaves the capsule stuck (referenced ConfigMap "not found").
//
// Note simple-fee-schedules is intentionally singular "fee" here even though the
// ConfigKey is "simple-fees-schedules": the operator/example use the singular
// form for the resource name.
var consensusConfigCRSuffix = map[string]string{
	ConfigKeyLog4j2:              "log4j2",
	ConfigKeySettings:            "settings",
	ConfigKeyAppProperties:       "application-properties",
	ConfigKeyAppOverride:         "application-override-properties",
	ConfigKeyApiPermission:       "api-permission-properties",
	ConfigKeyBootstrap:           "bootstrap-properties",
	ConfigKeyNodeProperties:      "node-properties",
	ConfigKeyFeeSchedules:        "fee-schedules",
	ConfigKeySimpleFeesSchedules: "simple-fee-schedules",
	ConfigKeyThrottles:           "throttles",
	ConfigKeyBlockNodes:          "block-nodes",
}

// ConsensusConfigCRName returns the config CR / managed ConfigMap / capsule ref
// name for a config key on a node, e.g.
// ConsensusConfigCRName("node0", ConfigKeyAppProperties) == "node0-application-properties".
func ConsensusConfigCRName(scope, configKey string) string {
	return fmt.Sprintf("%s-%s", scope, consensusConfigCRSuffix[configKey])
}

// Config content sources, stored in ConfigSources and ConfigHashEntry.Source.
const (
	ConfigSourcePackage  = "deployment-package"
	ConfigSourceEmbedded = "embedded"
)

// ConsensusNodeScope returns the node-local scope for a consensus node (e.g.
// "node0"). Use this for names of resources that live inside the node's
// namespace (secrets, config CR refs, secret key filenames) — the namespace
// already disambiguates them across orbits, so they stay node-local.
func ConsensusNodeScope(nodeId int64) string {
	return fmt.Sprintf("node%d", nodeId)
}

// ConsensusNodeStateKey returns the orbit-qualified key for solo-weaver's local
// state and reality maps (e.g. "hiero-network-1/node0"). Because the same
// nodeId can be deployed into multiple orbits in one cluster, the local state
// key must include the namespace/orbit — a bare "node0" would collide across
// orbits and overwrite entries in state.yaml. This is NOT a cluster resource
// name; it never appears on any Kubernetes object.
func ConsensusNodeStateKey(namespace string, nodeId int64) string {
	return fmt.Sprintf("%s/%s", namespace, ConsensusNodeScope(nodeId))
}

// ConsensusCapsuleName returns the CR name for a ConsensusCapsule (e.g. "myorbit-consensus-0").
func ConsensusCapsuleName(orbitName string, nodeId int64) string {
	return fmt.Sprintf("%s-consensus-%d", orbitName, nodeId)
}

// Validate checks that required fields are present.
func (c *ConsensusNodeInputs) Validate() error {
	if c.Namespace == "" {
		return errorx.IllegalArgument.New("--namespace is required")
	}
	if c.AccountId == "" {
		return errorx.IllegalArgument.New("--account-id is required")
	}
	if c.Weight <= 0 {
		return errorx.IllegalArgument.New("--weight must be positive")
	}
	return nil
}
