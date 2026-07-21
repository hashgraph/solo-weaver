// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"github.com/automa-saga/daemonkit"
	"github.com/joomcode/errorx"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// ComponentConfig holds inputs needed to build the block-node component.
type ComponentConfig struct {
	TrafficShaperEnabled bool

	// KubeconfigPath is the path to the BN-scoped kubeconfig. Required when
	// TrafficShaperEnabled is true (used to exec into BN pods to resolve veths).
	KubeconfigPath string

	// Namespace is the Kubernetes namespace (orbit) where BN pods run.
	// Required when TrafficShaperEnabled is true.
	Namespace string
}

// ComponentResult contains the monitors built by NewComponent and a reference
// to the TrafficShaperMonitor (when enabled) so daemon.go can wire the HTTP
// handler with the per-component StatusTracker closure after the component is
// assembled.
type ComponentResult struct {
	// Monitors is the ordered slice of monitors to run under the supervisor.
	Monitors []daemonkit.MonitorRunner

	// TrafficShaperMonitor is non-nil when the traffic-shaper monitor is enabled.
	// daemon.go uses this to construct BlockNodeHandler with the correct
	// trafficShaperStateFn closure after the component's StatusTracker is created.
	TrafficShaperMonitor *TrafficShaperMonitor
}

// NewComponent constructs all enabled monitors for the block-node component
// and returns them alongside any references needed for HTTP handler wiring.
func NewComponent(cfg ComponentConfig) (ComponentResult, error) {
	var monitors []daemonkit.MonitorRunner

	var tsm *TrafficShaperMonitor
	if cfg.TrafficShaperEnabled {
		resolver, err := NewVethResolver(VethResolverConfig{
			KubeconfigPath: cfg.KubeconfigPath,
		})
		if err != nil {
			return ComponentResult{}, err
		}
		client, err := newKubeClient(cfg.KubeconfigPath)
		if err != nil {
			return ComponentResult{}, err
		}
		tsm = NewTrafficShaperMonitor(resolver, client, cfg.Namespace)
		monitors = append(monitors, tsm)
	}

	return ComponentResult{
		Monitors:             monitors,
		TrafficShaperMonitor: tsm,
	}, nil
}

// newKubeClient builds a typed Kubernetes client from the BN-scoped kubeconfig.
// The pod watcher uses it to list/watch BN pods; the veth resolver builds its
// own client from the same kubeconfig for the SPDY exec path.
func newKubeClient(kubeconfigPath string) (kubernetes.Interface, error) {
	restCfg, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "load kubeconfig %s", kubeconfigPath)
	}
	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "build k8s client for pod watcher")
	}
	return client, nil
}
