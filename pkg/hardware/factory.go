// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// SupportedNodeTypes returns all supported node types
func SupportedNodeTypes() []string {
	return []string{models.NodeTypeBlock, models.NodeTypeConsensus}
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
	for _, supported := range models.SupportedProfiles() {
		if normalized == supported {
			return true
		}
	}
	return false
}

// CreateNodeSpec creates the appropriate node spec based on a DeploymentSpec and host profile.
// This function uses the RequirementsProvider registry to look up requirements based on
// the node type, separating the concerns of node type (what kind of node) and
// profile (deployment environment).
func CreateNodeSpec(spec DeploymentSpec, hostProfile HostProfile) (Spec, error) {
	normalizedNodeType := strings.ToLower(spec.NodeType)
	normalizedProfile := strings.ToLower(spec.Profile)

	// Validate node type
	if !IsValidNodeType(normalizedNodeType) {
		return nil, errorx.IllegalArgument.New("unsupported node type: %q. Supported types: %v", spec.NodeType, SupportedNodeTypes())
	}

	// Validate profile
	if !IsValidProfile(normalizedProfile) {
		return nil, errorx.IllegalArgument.New("unsupported profile: %q. Supported profiles: %v", spec.Profile, models.SupportedProfiles())
	}

	normalizedSpec := DeploymentSpec{
		NodeType: normalizedNodeType,
		Profile:  normalizedProfile,
		Options:  spec.Options,
	}

	nodeSpec, err := NewNodeSpec(normalizedSpec, hostProfile)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to create node spec")
	}

	return nodeSpec, nil
}
