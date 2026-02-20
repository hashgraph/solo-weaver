// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"strings"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/joomcode/errorx"
)

// SupportedNodeTypes returns all supported node types
func SupportedNodeTypes() []string {
	return []string{core.NodeTypeBlock, core.NodeTypeConsensus}
}

// SupportedProfiles returns all supported deployment profiles
func SupportedProfiles() []string {
	return []string{core.ProfileLocal, core.ProfilePerfnet, core.ProfileTestnet, core.ProfilePreviewnet, core.ProfileMainnet}
}

// IsValidNodeType checks if the given node type is supported
func IsValidNodeType(nodeType string) bool {
	normalized := strings.ToLower(nodeType)
	for _, supported := range SupportedNodeTypes() {
		if normalized == supported {
			return true
		}
	}
	return false
}

// IsValidProfile checks if the given profile is supported
func IsValidProfile(profile string) bool {
	normalized := strings.ToLower(profile)
	for _, supported := range SupportedProfiles() {
		if normalized == supported {
			return true
		}
	}
	return false
}

// CreateNodeSpec creates the appropriate node spec based on node type, profile and host profile.
// This function uses a requirements registry that maps (nodeType, profile) combinations
// to their specific hardware requirements, properly separating the concerns of
// node type (what kind of node) and profile (deployment environment).
func CreateNodeSpec(nodeType string, profile string, hostProfile HostProfile) (Spec, error) {
	normalizedNodeType := strings.ToLower(nodeType)
	normalizedProfile := strings.ToLower(profile)

	// Validate node type
	if !IsValidNodeType(normalizedNodeType) {
		return nil, errorx.IllegalArgument.New("unsupported node type: %q. Supported types: %v", nodeType, SupportedNodeTypes())
	}

	// Validate profile
	if !IsValidProfile(normalizedProfile) {
		return nil, errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v", profile, SupportedProfiles())
	}

	// Use the new unified node spec that looks up requirements from the registry
	spec, err := NewNodeSpec(normalizedNodeType, normalizedProfile, hostProfile)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to create node spec")
	}

	return spec, nil
}
