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
	CustomRules    string // Pre-rendered Alloy rule {} blocks for per-remote relabel injection
}

// AlloyData contains data for rendering Alloy configuration templates.
// Used by both the remotes template and individual module templates.
// Per-remote custom labels are handled via AlloyRemote.CustomRules.
type AlloyData struct {
	ClusterName       string
	PrometheusRemotes []AlloyRemote
	LokiRemotes       []AlloyRemote
}
