// SPDX-License-Identifier: Apache-2.0

package models

import (
	"reflect"

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

// Equal returns true if two ClusterInfo values are equal.
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

	if len(c.Clusters) != len(other.Clusters) || len(c.Contexts) != len(other.Contexts) {
		return false
	}

	for name, cluster := range c.Clusters {
		otherCluster, ok := other.Clusters[name]
		if !ok {
			return false
		}
		if cluster == nil || otherCluster == nil {
			if cluster != otherCluster {
				return false
			}
			continue
		}
		if !reflect.DeepEqual(*cluster, *otherCluster) {
			return false
		}
	}

	for name, ctx := range c.Contexts {
		otherCtx, ok := other.Contexts[name]
		if !ok {
			return false
		}
		if ctx == nil || otherCtx == nil {
			if ctx != otherCtx {
				return false
			}
			continue
		}
		if !reflect.DeepEqual(*ctx, *otherCtx) {
			return false
		}
	}

	return true
}
