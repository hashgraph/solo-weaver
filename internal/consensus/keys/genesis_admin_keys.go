// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/joomcode/errorx"
)

// NodeAdminKeysPath is where a deployment package sets each genesis node's admin key.
var NodeAdminKeysPath = filepath.Join("data", "config", "node-admin-keys.json")

// genesisNetwork is the subset of genesis-network.json (Network proto, JSON) read here.
type genesisNetwork struct {
	NodeMetadata []struct {
		RosterEntry *struct {
			NodeID json.Number `json:"nodeId"`
		} `json:"rosterEntry"`
		Node *struct {
			NodeID   json.Number     `json:"nodeId"`
			AdminKey json.RawMessage `json:"adminKey"`
		} `json:"node"`
	} `json:"nodeMetadata"`
}

// ValidateGenesisAdminKeys checks that every genesis node gets its own admin key.
// A node's key is node-admin-keys.json[node] if set, else genesis-network.json adminKey.
// Every node needs a key, keys set in both files must match, and every
// node-admin-keys.json entry must be a roster node. Empty nodeAdminKeysJSON means no file.
func ValidateGenesisAdminKeys(genesisNetworkJSON, nodeAdminKeysJSON string) error {
	roster, err := parseGenesisAdminKeys(genesisNetworkJSON)
	if err != nil {
		return err
	}
	overrides := map[int64][]byte{}
	if strings.TrimSpace(nodeAdminKeysJSON) != "" {
		if overrides, err = parseNodeAdminKeys(nodeAdminKeysJSON); err != nil {
			return err
		}
	}

	var missing, mismatched, unknown []int64
	for id, genesisKey := range roster {
		override, ok := overrides[id]
		switch {
		case !ok && len(genesisKey) == 0:
			missing = append(missing, id)
		case ok && len(genesisKey) > 0 && !bytes.Equal(override, genesisKey):
			mismatched = append(mismatched, id)
		}
	}
	for id := range overrides {
		if _, ok := roster[id]; !ok {
			unknown = append(unknown, id)
		}
	}

	var problems []string
	if len(missing) > 0 {
		problems = append(problems, "no admin key for node(s) "+joinIDs(missing)+
			" (set it in node-admin-keys.json or genesis-network.json adminKey)")
	}
	if len(mismatched) > 0 {
		problems = append(problems, "node-admin-keys.json and genesis-network.json adminKey differ for node(s) "+joinIDs(mismatched))
	}
	if len(unknown) > 0 {
		problems = append(problems, "node-admin-keys.json has node(s) "+joinIDs(unknown)+" that are not in genesis-network.json")
	}
	if len(problems) > 0 {
		return errorx.IllegalArgument.New("genesis admin keys are invalid: %s", strings.Join(problems, "; "))
	}
	return nil
}

// parseGenesisAdminKeys returns each roster node's raw Ed25519 admin key (nil when unset).
func parseGenesisAdminKeys(data string) (map[int64][]byte, error) {
	var gn genesisNetwork
	if err := json.Unmarshal([]byte(data), &gn); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "genesis-network.json is not valid JSON")
	}
	if len(gn.NodeMetadata) == 0 {
		return nil, errorx.IllegalFormat.New("genesis-network.json has no nodeMetadata entries")
	}

	roster := make(map[int64][]byte, len(gn.NodeMetadata))
	for i, meta := range gn.NodeMetadata {
		var rosterRaw, nodeRaw json.Number
		if meta.RosterEntry != nil {
			rosterRaw = meta.RosterEntry.NodeID
		}
		if meta.Node != nil {
			nodeRaw = meta.Node.NodeID
		}
		id, err := genesisNodeID(i, rosterRaw, nodeRaw)
		if err != nil {
			return nil, err
		}
		if _, dup := roster[id]; dup {
			return nil, errorx.IllegalFormat.New("genesis-network.json lists node %d more than once", id)
		}
		var key []byte
		if meta.Node != nil {
			if key, err = genesisEd25519Key(meta.Node.AdminKey); err != nil {
				return nil, errorx.IllegalFormat.Wrap(err, "genesis-network.json adminKey of node %d", id)
			}
		}
		roster[id] = key
	}
	return roster, nil
}

// genesisNodeID returns the entry's node ID. Proto JSON omits a zero nodeId, so a
// missing value is node 0; when both nodeIds are set, they must be equal.
func genesisNodeID(i int, rosterRaw, nodeRaw json.Number) (int64, error) {
	rosterID, err := parseNodeID(string(rosterRaw))
	if err != nil {
		return 0, errorx.IllegalFormat.Wrap(err, "genesis-network.json nodeMetadata[%d] has an invalid rosterEntry nodeId", i)
	}
	nodeID, err := parseNodeID(string(nodeRaw))
	if err != nil {
		return 0, errorx.IllegalFormat.Wrap(err, "genesis-network.json nodeMetadata[%d] has an invalid node nodeId", i)
	}
	switch {
	case rosterRaw == "":
		return nodeID, nil
	case nodeRaw != "" && rosterID != nodeID:
		return 0, errorx.IllegalFormat.New(
			"genesis-network.json nodeMetadata[%d] has rosterEntry nodeId %d but node nodeId %d", i, rosterID, nodeID)
	}
	return rosterID, nil
}

// genesisEd25519Key decodes a proto JSON Key ({"ed25519": "<base64>"}); nil when unset.
func genesisEd25519Key(raw json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil, nil
	}
	// HIP-1494's example shows a plain base64 string; proto JSON needs an object.
	if trimmed[0] != '{' {
		return nil, errorx.IllegalFormat.New(
			`adminKey must be an object like {"ed25519": "<base64>"}, not %s`, jsonKind(trimmed[0]))
	}
	var key map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &key); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "adminKey is not a valid JSON object")
	}
	if len(key) == 0 {
		return nil, nil
	}
	rawField, ok := key["ed25519"]
	if !ok || len(key) != 1 {
		return nil, errorx.IllegalFormat.New("only an Ed25519 key is accepted")
	}
	var b64 string
	if err := json.Unmarshal(rawField, &b64); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "ed25519 value is not a string")
	}
	if b64 == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "ed25519 value is not valid base64")
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, errorx.IllegalFormat.New("ed25519 key is %d bytes, want %d", len(raw), ed25519.PublicKeySize)
	}
	return raw, nil
}

// jsonKind names the JSON value type that starts with c.
func jsonKind(c byte) string {
	switch c {
	case '"':
		return "a string"
	case '[':
		return "an array"
	case 't', 'f':
		return "a boolean"
	default:
		return "a number"
	}
}

// parseNodeAdminKeys reads node-admin-keys.json ({"<nodeId>": "<hex Ed25519>"}).
// The node ignores the whole file on a parse error, so this check is strict.
func parseNodeAdminKeys(data string) (map[int64][]byte, error) {
	var entries map[string]string
	if err := json.Unmarshal([]byte(data), &entries); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "node-admin-keys.json must be a JSON object of node ID to hex Ed25519 key")
	}
	keys := make(map[int64][]byte, len(entries))
	for name, value := range entries {
		id, err := parseNodeID(name)
		if err != nil {
			return nil, errorx.IllegalFormat.Wrap(err, "node-admin-keys.json has an invalid node ID %q", name)
		}
		raw, err := hex.DecodeString(value)
		if err != nil {
			return nil, errorx.IllegalFormat.Wrap(err, "node-admin-keys.json key of node %d is not valid hex", id)
		}
		if err := checkAdminPublicLen(len(raw)); err != nil {
			return nil, errorx.Decorate(err, "node-admin-keys.json key of node %d", id)
		}
		if _, dup := keys[id]; dup {
			return nil, errorx.IllegalFormat.New("node-admin-keys.json lists node %d more than once", id)
		}
		keys[id] = raw
	}
	return keys, nil
}

// parseNodeID parses a non-negative node ID; empty is node 0 (proto JSON default).
func parseNodeID(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, errorx.IllegalFormat.Wrap(err, "node ID %q is not a number", s)
	}
	if id < 0 {
		return 0, errorx.IllegalFormat.New("node ID %d is negative", id)
	}
	return id, nil
}

func joinIDs(ids []int64) string {
	slices.Sort(ids)
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ", ")
}
