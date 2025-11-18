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

// CreateNodeSpec creates the appropriate node spec based on node type and host profile
func CreateNodeSpec(nodeType string, hostProfile HostProfile) (Spec, error) {
	normalized := strings.ToLower(nodeType)

	switch normalized {
	case core.NodeTypeLocal:
		return NewLocalNodeSpec(hostProfile), nil
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
