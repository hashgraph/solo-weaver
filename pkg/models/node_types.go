// SPDX-License-Identifier: Apache-2.0

package models

const (
	// Node types
	NodeTypeLocal     = "local"
	NodeTypeBlock     = "block"
	NodeTypeConsensus = "consensus"
	NodeTypeMirror    = "mirror"
	NodeTypeRelay     = "relay"
)

func AllNodeTypes() []string {
	return []string{
		NodeTypeLocal,
		NodeTypeBlock,
		NodeTypeConsensus,
		NodeTypeMirror,
		NodeTypeRelay,
	}
}
