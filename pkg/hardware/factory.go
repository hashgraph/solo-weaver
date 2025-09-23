package hardware

import (
	"strings"

	"github.com/joomcode/errorx"
)

// SupportedNodeTypes returns all supported node types
func SupportedNodeTypes() []NodeType {
	return []NodeType{NodeTypeLocal, NodeTypeBlock, NodeTypeConsensus}
}

// IsValidNodeType checks if the given node type is supported
func IsValidNodeType(nodeType string) bool {
	normalized := NodeType(strings.ToLower(nodeType))
	for _, supported := range SupportedNodeTypes() {
		if normalized == supported {
			return true
		}
	}
	return false
}

// CreateNodeSpec creates the appropriate node spec based on node type and host profile
func CreateNodeSpec(nodeType string, hostProfile HostProfile) (Spec, error) {
	normalized := NodeType(strings.ToLower(nodeType))

	switch normalized {
	case NodeTypeLocal:
		return NewLocalNodeSpec(hostProfile), nil
	case NodeTypeBlock:
		return NewBlockNodeSpec(hostProfile), nil
	case NodeTypeConsensus:
		return NewConsensusNodeSpec(hostProfile), nil
	default:
		supportedTypes := make([]string, len(SupportedNodeTypes()))
		for i, t := range SupportedNodeTypes() {
			supportedTypes[i] = string(t)
		}
		return nil, errorx.IllegalArgument.New("unsupported node type: %s. Supported types: %v", nodeType, supportedTypes)
	}
}
