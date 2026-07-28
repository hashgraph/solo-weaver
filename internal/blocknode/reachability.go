// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/network"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// BlockNodePublicPort is the well-known TCP port the block-node gRPC service
// listens on (the chart's `service.port` default; every SDK and tool assumes
// it). The reachability probe prefers the LoadBalancer Service's own advertised
// port and falls back to this constant only when the Service exposes no port, so
// a chart that publishes the service on a non-default port is probed on the port
// it actually announces rather than a stale hardcoded number.
const BlockNodePublicPort int64 = 40840

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
			m.blockNodeInputs.Namespace).
			WithProperty(models.ErrPropertyResolution, []string{
				"The block node's main Service is not a LoadBalancer, so there is no external endpoint to probe.",
				"This usually means the values set 'service.type: ClusterIP' (or another non-LoadBalancer type) while --load-balancer-enabled is true (the default), which is what enables this probe.",
				"If a ClusterIP main service is intended, re-run with --load-balancer-enabled=false to skip the reachability probe.",
				"If external reachability is intended, remove the service.type override so the main Service is a LoadBalancer.",
				"For a split topology (chart owns a separate external LoadBalancer), set loadBalancer.enabled: true in your -f values so the chart renders the '-external' Service.",
			})
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

	return ip, loadBalancerPort(lb), nil
}

// loadBalancerPort returns the port the LoadBalancer Service advertises (its
// first spec.ports entry), falling back to the well-known BlockNodePublicPort
// when the Service exposes no ports — so the probe targets the port the Service
// actually announces rather than a hardcoded number.
func loadBalancerPort(lb *unstructured.Unstructured) int64 {
	ports, found, _ := unstructured.NestedSlice(lb.Object, "spec", "ports")
	if !found || len(ports) == 0 {
		return BlockNodePublicPort
	}
	entry, ok := ports[0].(map[string]interface{})
	if !ok {
		return BlockNodePublicPort
	}
	if port, ok, _ := unstructured.NestedInt64(entry, "port"); ok && port > 0 {
		return port
	}
	return BlockNodePublicPort
}
