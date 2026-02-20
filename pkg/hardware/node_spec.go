// SPDX-License-Identifier: Apache-2.0

package hardware

import "fmt"

// nodeSpec implements Spec by combining requirements from the registry
// with the actual host profile for validation.
type nodeSpec struct {
	baseNode
	rawNodeType string
	profile     string
}

// Ensure nodeSpec implements Spec
var _ Spec = (*nodeSpec)(nil)

// NewNodeSpec creates a node specification for the given node type, profile, and host profile.
// Requirements are looked up from the registry based on (nodeType, profile).
func NewNodeSpec(nodeType, profile string, hostProfile HostProfile) (Spec, error) {
	requirements, found := GetRequirements(nodeType, profile)
	if !found {
		return nil, fmt.Errorf("no requirements defined for node type %q with profile %q", nodeType, profile)
	}

	return &nodeSpec{
		baseNode: baseNode{
			nodeType:            formatDisplayName(nodeType, profile),
			actualHostProfile:   hostProfile,
			minimalRequirements: requirements,
		},
		rawNodeType: nodeType,
		profile:     profile,
	}, nil
}

// formatDisplayName creates a human-readable display name
func formatDisplayName(nodeType, profile string) string {
	return fmt.Sprintf("%s Node (%s)", capitalize(nodeType), capitalize(profile))
}

// capitalize capitalizes the first letter of a string
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	// Simple ASCII uppercase for first char
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-('a'-'A')) + s[1:]
	}
	return s
}

// GetProfile returns the deployment profile
func (n *nodeSpec) GetProfile() string {
	return n.profile
}

// GetRawNodeType returns the raw node type (e.g., "block", "consensus")
func (n *nodeSpec) GetRawNodeType() string {
	return n.rawNodeType
}
