// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"context"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/state"
)

// FirewallReader reads the host firewall as it is, for the drift producer. It
// cannot fail: a table it could not read is reported as unobserved, never as
// absent. A nil reading means nothing was read.
type FirewallReader func(ctx context.Context) *state.HostFirewallObservation

// firewallReader adapts the firewall package's read to a FirewallReader.
func firewallReader(observe func(context.Context) firewall.Observation) FirewallReader {
	return func(ctx context.Context) *state.HostFirewallObservation {
		o := observe(ctx)
		observed := &state.HostFirewallObservation{
			Unobserved:       o.Unobserved,
			TablePresent:     o.TablePresent,
			ConfigPresent:    o.ConfigPresent,
			ConfigUnreadable: o.ConfigUnreadable,
		}
		for _, f := range o.Fields {
			observed.Fields = append(observed.Fields, state.HostFirewallFieldObservation{
				Field: f.Field, Config: f.Config, Live: f.Live,
			})
		}
		for _, r := range o.Rules {
			observed.Rules = append(observed.Rules, state.HostFirewallRuleObservation{
				Name: r.Name, InConfig: r.InConfig, Live: r.Live,
			})
		}
		return observed
	}
}
