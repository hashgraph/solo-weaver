// SPDX-License-Identifier: Apache-2.0

package labels

import (
	"strings"
	"unicode"
)

// OpsProfile adds labels used by DevOps tooling.
// Copy this file as a template when creating a new label profile.
type OpsProfile struct{}

func init() {
	Register(OpsProfile{})
}

func (OpsProfile) Name() string { return "ops" }

// Labels returns the complete label set for the ops profile.
//
// Labels added (from LabelInput):
//   - cluster        = ClusterName
//   - environment    = DeployProfile
//   - instance_type  = alphabetic prefix of first cluster name segment (e.g. "lfh")
//   - inventory_name = full cluster name (for DevOps inventory systems)
//   - ip             = MachineIP (when available)
func (OpsProfile) Labels(input LabelInput) map[string]string {
	labels := ParseClusterName(input.ClusterName)

	if input.ClusterName != "" {
		labels["cluster"] = input.ClusterName
		labels["inventory_name"] = input.ClusterName
	}

	if input.DeployProfile != "" {
		labels["environment"] = input.DeployProfile
	}

	if input.MachineIP != "" {
		labels["ip"] = input.MachineIP
	}

	return labels
}

// ParseClusterName extracts standardized label values from a cluster name
// following the convention: <instance>-<environment>-<suffix>
// For example: "lfh02-previewnet-blocknode" yields:
//   - instance_type = "lfh"  (alphabetic prefix of first segment)
//
// Note: The "cluster" label is not included here because it is attached via
// CustomRules generated from the resolved label profile (see OpsProfile.Labels).
// Similarly, the "environment" label is provided by deployProfile rather than
// being derived from the cluster name.
func ParseClusterName(clusterName string) map[string]string {
	labels := make(map[string]string)
	if clusterName == "" {
		return labels
	}

	parts := strings.SplitN(clusterName, "-", 3)

	// First segment: derive instance_type as the alphabetic prefix (e.g., "lfh" from "lfh02")
	if len(parts) >= 1 && parts[0] != "" {
		instanceType := extractAlphaPrefix(parts[0])
		if instanceType != "" {
			labels["instance_type"] = instanceType
		}
	}

	return labels
}

// extractAlphaPrefix returns the leading alphabetic characters of s.
// For example, "lfh02" → "lfh", "abc" → "abc", "123" → "".
func extractAlphaPrefix(s string) string {
	for i, r := range s {
		if !unicode.IsLetter(r) {
			return s[:i]
		}
	}
	return s
}
