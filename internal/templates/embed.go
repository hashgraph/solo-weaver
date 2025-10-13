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
