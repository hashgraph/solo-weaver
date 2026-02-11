// SPDX-License-Identifier: Apache-2.0

package templates

import (
	"embed"
)

//go:embed files/*
var Files embed.FS

type KubeadmInitData struct {
	KubeBootstrapToken string
	SandboxDir         string
	MachineIP          string
	Hostname           string
	KubernetesVersion  string
}

type MetallbData struct {
	MachineIP string
}

// AlloyRemote represents a single remote endpoint for Prometheus or Loki.
type AlloyRemote struct {
	Name           string // Unique identifier for this remote (used in Alloy config block names)
	URL            string // Remote write URL
	Username       string // Basic auth username
	PasswordEnvVar string // Environment variable name containing the password
}

// AlloyData contains data for rendering the main Alloy configuration templates.
type AlloyData struct {
	ClusterName         string
	PrometheusRemotes   []AlloyRemote
	LokiRemotes         []AlloyRemote
	PrometheusForwardTo string // Comma-separated list of prometheus remote receivers
	LokiForwardTo       string // Comma-separated list of loki remote receivers
	MonitorBlockNode    bool
}

// AlloyModuleData contains data for rendering individual Alloy module templates.
// This is used for modules that only need cluster name and forward-to receivers.
type AlloyModuleData struct {
	ClusterName         string
	PrometheusForwardTo string // Comma-separated list of prometheus remote receivers
	LokiForwardTo       string // Comma-separated list of loki remote receivers
}
