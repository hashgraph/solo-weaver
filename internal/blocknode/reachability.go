// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"net"
	"reflect"
	"strconv"
	"time"

	"github.com/hashgraph/solo-weaver/internal/cilium"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/network"
	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// BlockNodePublicPort is the well-known TCP port the block-node gRPC service
// listens on. It's an ecosystem-wide contract (the chart defaults to it, every
// SDK and tool assumes it), so the reachability probe dials it directly instead
// of trying to look the port up by name on the Service — chart versions have
// used `http`, `grpc`, and other names for it, and matching any of those is
// more fragile than dialing the well-known number.
const BlockNodePublicPort int64 = 40840

// SnapshotServices records the current spec of every Service in the block-node
// namespace on the Manager. Call this before any operation that could mutate
// Services (e.g. helm upgrade) so that BlockNodeServicesChanged / the conditional
// Cilium restart can later decide whether anything actually changed.
//
// Snapshots overwrite any previous snapshot on the same Manager.
func (m *Manager) SnapshotServices(ctx context.Context) error {
	services, err := m.kubeClient.List(ctx, kube.KindService, m.blockNodeInputs.Namespace, kube.WaitOptions{})
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to list services in namespace %s", m.blockNodeInputs.Namespace)
	}

	snapshot := make(map[string]map[string]interface{}, len(services.Items))
	for i := range services.Items {
		svc := &services.Items[i]
		spec, found, _ := unstructured.NestedMap(svc.Object, "spec")
		if !found {
			// Service with no spec is malformed but recoverable — record empty map.
			spec = map[string]interface{}{}
		}
		snapshot[svc.GetName()] = spec
	}

	m.preUpgradeServiceSpecs = snapshot
	m.logger.Debug().
		Int("services", len(snapshot)).
		Str("namespace", m.blockNodeInputs.Namespace).
		Msg("Snapshotted block-node Services before mutation")
	return nil
}

// BlockNodeServicesChanged returns true if the current set of Services in the
// block-node namespace differs from the snapshot taken by SnapshotServices, by
// any of: Service added, Service removed, Service spec mutated.
//
// If SnapshotServices was never called, returns true conservatively — the caller
// cannot prove that nothing changed, so the safe choice is to restart Cilium.
func (m *Manager) BlockNodeServicesChanged(ctx context.Context) (bool, error) {
	if m.preUpgradeServiceSpecs == nil {
		m.logger.Debug().Msg("No pre-upgrade Service snapshot available; assuming Services changed")
		return true, nil
	}

	services, err := m.kubeClient.List(ctx, kube.KindService, m.blockNodeInputs.Namespace, kube.WaitOptions{})
	if err != nil {
		return false, errorx.IllegalState.Wrap(err, "failed to list services for diff in namespace %s", m.blockNodeInputs.Namespace)
	}

	current := make(map[string]map[string]interface{}, len(services.Items))
	for i := range services.Items {
		svc := &services.Items[i]
		spec, found, _ := unstructured.NestedMap(svc.Object, "spec")
		if !found {
			spec = map[string]interface{}{}
		}
		current[svc.GetName()] = spec
	}

	return diffServiceSpecs(m.preUpgradeServiceSpecs, current), nil
}

// diffServiceSpecs returns true when the two name → spec snapshots differ in
// membership or in any individual spec map.
func diffServiceSpecs(before, after map[string]map[string]interface{}) bool {
	if len(before) != len(after) {
		return true
	}
	for name, prev := range before {
		curr, ok := after[name]
		if !ok {
			return true
		}
		if !reflect.DeepEqual(prev, curr) {
			return true
		}
	}
	return false
}

// RestartCiliumDaemonSetIfServicesChanged compares the current block-node Service
// state with the snapshot recorded by SnapshotServices and restarts the Cilium
// agent DaemonSet only if anything changed. Most upgrades don't touch Services
// and shouldn't pay the ~30s Cilium-restart cost; topology-affecting upgrades
// must restart Cilium to refresh its eBPF reconciler (see issue #619).
func (m *Manager) RestartCiliumDaemonSetIfServicesChanged(ctx context.Context) error {
	changed, err := m.BlockNodeServicesChanged(ctx)
	if err != nil {
		return err
	}
	if !changed {
		m.logger.Info().Msg("No block-node Service changes detected; skipping Cilium DaemonSet restart")
		return nil
	}
	m.logger.Info().Msg("Block-node Service mutation detected; restarting Cilium to refresh eBPF reconciler")
	return cilium.RestartAgentDaemonSet(ctx, m.kubeClient, cilium.DefaultRolloutTimeout)
}

// VerifyExternalReachable opens a TCP connection from the host running
// solo-provisioner to the block-node LoadBalancer Service's external endpoint
// and closes it immediately. No-ops when LoadBalancerEnabled is false (e.g.
// local profile, no MetalLB pool).
//
// The probe converts the silent failure mode described in issue #619 — pod
// healthy, kubectl happy, traffic blackholed — into an immediate workflow error
// regardless of whether the cause is Cilium, MetalLB, the chart, or a firewall.
//
// Traffic from this process to the LB IP traverses the same MetalLB-ARP +
// Cilium-DNAT path that any external client would hit, so a failure here is the
// same failure an outside caller would experience.
func (m *Manager) VerifyExternalReachable(ctx context.Context) error {
	if !m.blockNodeInputs.LoadBalancerEnabled {
		m.logger.Debug().Msg("LoadBalancer not enabled; skipping reachability probe")
		return nil
	}

	ip, port, err := m.findLoadBalancerEndpoint(ctx)
	if err != nil {
		return err
	}

	addr := net.JoinHostPort(ip, strconv.FormatInt(port, 10))
	overallTimeout := time.Duration(ReachabilityProbeTimeoutSec) * time.Second

	m.logger.Info().
		Str("target", addr).
		Dur("timeout", overallTimeout).
		Msg("Probing block-node LoadBalancer reachability")

	attempts, err := network.ProbeTCP(ctx, addr, overallTimeout, ReachabilityProbeDialTimeout, ReachabilityProbeRetryDelay)
	if err != nil {
		return errorx.IllegalState.Wrap(err,
			"block node LoadBalancer at %s is not reachable from solo-provisioner after %d attempts in %s — "+
				"traffic is likely being dropped by Cilium or MetalLB",
			addr, attempts, overallTimeout)
	}
	m.logger.Info().
		Str("target", addr).
		Int("attempts", attempts).
		Msg("Block-node LoadBalancer is reachable")
	return nil
}

// findLoadBalancerEndpoint locates the block-node LoadBalancer Service in the
// configured namespace and returns its assigned external IP and public port.
//
// Shape B (weaver default) → single Service of type LoadBalancer.
// Shape A (operator-installed split topology) → the `-external` Service of type
// LoadBalancer alongside a ClusterIP main Service. Either is fine: the probe
// targets whichever Service is actually announcing the external IP.
func (m *Manager) findLoadBalancerEndpoint(ctx context.Context) (string, int64, error) {
	services, err := m.kubeClient.List(ctx, kube.KindService, m.blockNodeInputs.Namespace, kube.WaitOptions{})
	if err != nil {
		return "", 0, errorx.IllegalState.Wrap(err, "failed to list services in namespace %s", m.blockNodeInputs.Namespace)
	}

	var lb *unstructured.Unstructured
	for i := range services.Items {
		svc := &services.Items[i]
		svcType, _, _ := unstructured.NestedString(svc.Object, "spec", "type")
		if svcType == "LoadBalancer" {
			lb = svc
			break
		}
	}
	if lb == nil {
		return "", 0, errorx.IllegalState.New(
			"no LoadBalancer Service found in namespace %s; cannot probe reachability",
			m.blockNodeInputs.Namespace)
	}

	ingress, found, _ := unstructured.NestedSlice(lb.Object, "status", "loadBalancer", "ingress")
	if !found || len(ingress) == 0 {
		return "", 0, errorx.IllegalState.New(
			"LoadBalancer Service %s has no ingress IP assigned yet; MetalLB may have failed to allocate",
			lb.GetName())
	}
	ingressEntry, ok := ingress[0].(map[string]interface{})
	if !ok {
		return "", 0, errorx.IllegalState.New("loadBalancer.ingress[0] has unexpected shape on Service %s", lb.GetName())
	}
	ip, _, _ := unstructured.NestedString(ingressEntry, "ip")
	if ip == "" {
		return "", 0, errorx.IllegalState.New("loadBalancer.ingress[0].ip is empty on Service %s", lb.GetName())
	}

	return ip, BlockNodePublicPort, nil
}
