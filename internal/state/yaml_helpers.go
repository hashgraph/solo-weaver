// SPDX-License-Identifier: Apache-2.0

// yaml_helpers.go provides low-level yaml.Node manipulation utilities shared
// across state migrations.
//
// All functions in this file are pure (no file I/O) unless they explicitly
// say otherwise, making them straightforward to unit test.
//
// yaml.Node MappingNode layout reminder:
//
//	Content is a flat slice: [key₀, val₀, key₁, val₁, …]
//	Key and value nodes are both *yaml.Node.
//	Renaming a key means finding index i where Content[i].Value == oldKey
//	and updating Content[i].Value in-place.

package state

import (
	"gopkg.in/yaml.v3"
)

// ── yaml.Node helpers ─────────────────────────────────────────────────────────

// rootMappingNode unwraps a yaml.DocumentNode and returns its root MappingNode,
// or nil when doc is not a document node, has no content, or the root is not a
// mapping.
func rootMappingNode(doc *yaml.Node) *yaml.Node {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	n := doc.Content[0]
	if n.Kind != yaml.MappingNode {
		return nil
	}
	return n
}

// mappingValue returns the value node for key inside a MappingNode.
// Returns nil when node is not a mapping or key is absent.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// mappingScalar returns the scalar string for key inside a MappingNode.
// Returns "" when the key is absent.
func mappingScalar(node *yaml.Node, key string) string {
	v := mappingValue(node, key)
	if v == nil {
		return ""
	}
	return v.Value
}

// setMappingScalar updates the scalar value for an existing key inside a
// MappingNode. No-op when the key does not exist or node is nil.
func setMappingScalar(node *yaml.Node, key, value string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].Value = value
			return
		}
	}
}

// renameMappingKey renames the first occurrence of oldKey to newKey inside a
// MappingNode. Only the key node's Value is changed; the associated value node
// is untouched. No-op when oldKey is not found or node is nil.
func renameMappingKey(node *yaml.Node, oldKey, newKey string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == oldKey {
			node.Content[i].Value = newKey
			return
		}
	}
}
