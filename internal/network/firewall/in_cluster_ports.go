// SPDX-License-Identifier: Apache-2.0

package firewall

import "github.com/hashgraph/solo-weaver/pkg/models"

// legacyInClusterPorts is DefaultInClusterPorts before node-exporter's port
// joined it. Hosts set up earlier wrote this list out in full.
var legacyInClusterPorts = []int{6443, 4244, 7472, 10250}

// NodeExporterPortVerdict is what to do with an in-cluster port list written
// before node-exporter's port joined the defaults.
type NodeExporterPortVerdict int

const (
	// NodeExporterPortNoList: the list is empty and opens nothing. Adding the
	// port would open one, so leave it.
	NodeExporterPortNoList NodeExporterPortVerdict = iota
	// NodeExporterPortPresent: the list already includes the port.
	NodeExporterPortPresent
	// NodeExporterPortAdd: the list still holds every legacy default and lacks
	// the port.
	NodeExporterPortAdd
	// NodeExporterPortNarrowed: the list lacks a legacy default, so the operator
	// chose it. Leave it.
	NodeExporterPortNarrowed
)

// VerdictForNodeExporterPort judges an in-cluster port list of single ports or
// inclusive ranges. It rejects any invalid spec.
func VerdictForNodeExporterPort(specs []string) (NodeExporterPortVerdict, error) {
	for _, s := range specs {
		if err := validatePortSpec(s); err != nil {
			return NodeExporterPortNoList, err
		}
	}
	if len(specs) == 0 {
		return NodeExporterPortNoList, nil
	}
	if portSpecsCover(specs, models.NodeExporterPort) {
		return NodeExporterPortPresent, nil
	}
	for _, p := range legacyInClusterPorts {
		if !portSpecsCover(specs, p) {
			return NodeExporterPortNarrowed, nil
		}
	}
	return NodeExporterPortAdd, nil
}

// portSpecsCover reports whether any of specs includes port. specs must be
// valid.
func portSpecsCover(specs []string, port int) bool {
	for _, s := range specs {
		lo, hi, _ := parsePortSpec(s)
		if lo <= port && port <= hi {
			return true
		}
	}
	return false
}
