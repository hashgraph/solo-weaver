// SPDX-License-Identifier: Apache-2.0

package models

import (
	k8sversion "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/tools/clientcmd/api"
)

type ClusterInfo struct {
	KubeconfigEnv  string                  `yaml:"kubeconfigEnv" json:"kubeconfigEnv"`
	KubeconfigPath string                  `yaml:"kubeconfigPath" json:"kubeconfigPath"`
	ServerVersion  k8sversion.Info         `yaml:"serverVersion" json:"serverVersion"`
	Host           string                  `yaml:"host" json:"host"`
	Proxy          string                  `yaml:"proxy", json:"proxy"`
	Clusters       map[string]*api.Cluster `yaml:"clusters" json:"clusters"`
	Contexts       map[string]*api.Context `yaml:"contexts" json:"contexts"`
	CurrentContext string                  `yaml:"currentContext" json:"currentContext"`
	Namespace      string                  `yaml:"namespace" json:"namespace"`
}
