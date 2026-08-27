// SPDX-License-Identifier: Apache-2.0

package models

import (
	"fmt"

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
	HapiAppSecret string `json:"hapiAppSecret,omitempty"`

	UpgradeOperator bool `json:"upgradeOperator,omitempty"`

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

// Config content sources, stored in ConfigSources and ConfigHashEntry.Source.
const (
	ConfigSourcePackage  = "deployment-package"
	ConfigSourceEmbedded = "embedded"
)

// ConsensusNodeScope returns the canonical scope key for a consensus node (e.g. "node0").
func ConsensusNodeScope(nodeId int64) string {
	return fmt.Sprintf("node%d", nodeId)
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
