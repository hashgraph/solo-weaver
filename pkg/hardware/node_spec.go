// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"fmt"

	"github.com/joomcode/errorx"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// nodeSpec implements Spec by combining requirements from the provider registry
// with the actual host profile for validation.
type nodeSpec struct {
	baseNode
	rawNodeType string
	profile     string
}

// Ensure nodeSpec implements Spec
var _ Spec = (*nodeSpec)(nil)

// NewNodeSpec creates a node specification for the given DeploymentSpec and host profile.
// Requirements are computed by the registered RequirementsProvider for the node type.
func NewNodeSpec(spec DeploymentSpec, hostProfile HostProfile) (Spec, error) {
	providers := Providers()
	p, ok := providers[spec.NodeType]
	if !ok {
		return nil, errorx.IllegalArgument.New("no requirements provider registered for node type %q", spec.NodeType)
	}

	requirements, err := p.Compute(spec)
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "failed to compute requirements for node type %q with profile %q", spec.NodeType, spec.Profile)
	}

	return &nodeSpec{
		baseNode: baseNode{
			nodeType:            formatDisplayName(spec.NodeType, spec.Profile),
			actualHostProfile:   hostProfile,
			minimalRequirements: requirements,
		},
		rawNodeType: spec.NodeType,
		profile:     spec.Profile,
	}, nil
}

// formatDisplayName creates a human-readable display name
func formatDisplayName(nodeType, profile string) string {
	caser := cases.Title(language.Und)
	return fmt.Sprintf("%s Node (%s)", caser.String(nodeType), caser.String(profile))
}

// GetProfile returns the deployment profile
func (n *nodeSpec) GetProfile() string {
	return n.profile
}

// GetRawNodeType returns the raw node type (e.g., "block", "consensus")
func (n *nodeSpec) GetRawNodeType() string {
	return n.rawNodeType
}
