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

type AlloyData struct {
	ClusterName        string
	PrometheusURL      string
	PrometheusUsername string
	LokiURL            string
	LokiUsername       string
	MonitorBlockNode   bool
}
