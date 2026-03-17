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

// Equal returns true if two ClusterInfo values are equal,
// treating nil and empty maps as equivalent.
func (c ClusterInfo) Equal(other ClusterInfo) bool {
	if c.KubeconfigEnv != other.KubeconfigEnv ||
		c.KubeconfigPath != other.KubeconfigPath ||
		c.Host != other.Host ||
		c.Proxy != other.Proxy ||
		c.CurrentContext != other.CurrentContext ||
		c.Namespace != other.Namespace ||
		c.ServerVersion != other.ServerVersion {
		return false
	}
	if len(c.Clusters) != len(other.Clusters) {
		return false
	}
	for k, v := range c.Clusters {
		if other.Clusters[k] != v {
			return false
		}
	}
	if len(c.Contexts) != len(other.Contexts) {
		return false
	}
	for k, v := range c.Contexts {
		if other.Contexts[k] != v {
			return false
		}
	}
	return true
}
