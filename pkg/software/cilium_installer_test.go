// SPDX-License-Identifier: Apache-2.0

package software

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/stretchr/testify/require"
)

// Test_ciliumConfigTemplate_HostLegacyRoutingAndLoadBalancer pins the
// load-bearing Cilium ConfigMap values the BN traffic shaper depends on
// (issue #741): host legacy routing must be enabled and the load-balancer mode
// must stay DSR. These render statically (no template data), so a direct render
// is enough to catch an accidental flip.
func Test_ciliumConfigTemplate_HostLegacyRoutingAndLoadBalancer(t *testing.T) {
	rendered, err := templates.Render(ciliumTemplateFile, struct {
		SandboxDir string
		MachineIP  string
	}{
		SandboxDir: "/opt/weaver",
		MachineIP:  "10.0.0.1",
	})
	require.NoError(t, err)

	// enable-host-legacy-routing: "true" — rendered from bpf.hostLegacyRouting.
	require.Contains(t, rendered, "hostLegacyRouting: true")
	require.NotContains(t, rendered, "hostLegacyRouting: false")

	// loadBalancer.mode must remain DSR; BandwidthManager must not be enabled in
	// the template (it stays at the Cilium default of Disabled).
	require.Contains(t, rendered, "mode: dsr")
	require.NotContains(t, rendered, "bandwidthManager")
}
