package hardware

import (
	"strings"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/core"
)

// SupportedNodeTypes returns all supported node types
func SupportedNodeTypes() []string {
	return []string{core.NodeTypeLocal, core.NodeTypeBlock, core.NodeTypeConsensus}
}

// SupportedProfiles returns all supported deployment profiles
func SupportedProfiles() []string {
	return []string{core.ProfileLocal, core.ProfilePerfnet, core.ProfileTestnet, core.ProfileMainnet}
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

// CreateNodeSpec creates the appropriate node spec based on node type, profile and host profile
func CreateNodeSpec(nodeType string, profile string, hostProfile HostProfile) (Spec, error) {
	normalized := strings.ToLower(nodeType)
	normalizedProfile := strings.ToLower(profile)

	// For local profile, use local node specs regardless of node type
	if normalizedProfile == core.ProfileLocal {
		return NewLocalNodeSpec(hostProfile), nil
	}

	// For other profiles, use node-specific requirements
	switch normalized {
	case core.NodeTypeBlock:
		return NewBlockNodeSpec(hostProfile), nil
	case core.NodeTypeConsensus:
		return NewConsensusNodeSpec(hostProfile), nil
	default:
		supportedTypes := make([]string, len(SupportedNodeTypes()))
		for i, t := range SupportedNodeTypes() {
			supportedTypes[i] = string(t)
		}
		return nil, errorx.IllegalArgument.New("unsupported node type: %s. Supported types: %v", nodeType, supportedTypes)
	}
}
