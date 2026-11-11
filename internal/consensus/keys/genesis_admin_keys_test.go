// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Same key in both encodings, from hiero-consensus-node hedera-node/configuration/dev.
const (
	devKeyB64 = "CqjiEGTGHquG4qnBZFZbTnqaQUYQbgps0DqMOVoRDpI="
	devKeyHex = "0aa8e21064c61eab86e2a9c164565b4e7a9a4146106e0a6cd03a8c395a110e92"
)

func keyHex(b byte) string { return hex.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
func keyB64(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

// genesisNode renders one nodeMetadata entry like proto JSON: nodeId 0 is omitted.
func genesisNode(id int, adminKey string) string {
	rosterID := ""
	if id != 0 {
		rosterID = fmt.Sprintf(`"nodeId":"%d",`, id)
	}
	key := ""
	if adminKey != "" {
		key = fmt.Sprintf(`,"adminKey":%s`, adminKey)
	}
	return fmt.Sprintf(`{"rosterEntry":{%s"weight":"1"},"node":{"description":"node%d"%s}}`, rosterID, id+1, key)
}

func genesis(nodes ...string) string {
	return `{"nodeMetadata":[` + strings.Join(nodes, ",") + `]}`
}

func ed(b64 string) string { return fmt.Sprintf(`{"ed25519":%q}`, b64) }

func TestValidateGenesisAdminKeys_Valid(t *testing.T) {
	tests := []struct {
		name      string
		genesis   string
		adminKeys string
	}{
		{"all keys in genesis, no file", genesis(genesisNode(0, ed(keyB64(1))), genesisNode(1, ed(keyB64(2)))), ""},
		{"all keys in file only", genesis(genesisNode(0, ""), genesisNode(1, "")),
			fmt.Sprintf(`{"0":%q,"1":%q}`, keyHex(1), keyHex(2))},
		{"keys split across files", genesis(genesisNode(0, ed(keyB64(1))), genesisNode(1, "")),
			fmt.Sprintf(`{"1":%q}`, keyHex(2))},
		{"same key in both encodings", genesis(genesisNode(0, ed(devKeyB64))), fmt.Sprintf(`{"0":%q}`, devKeyHex)},
		{"empty ed25519 value counts as unset", genesis(genesisNode(0, ed(""))), fmt.Sprintf(`{"0":%q}`, keyHex(1))},
		{"empty adminKey object counts as unset", genesis(genesisNode(0, `{}`)), fmt.Sprintf(`{"0":%q}`, keyHex(1))},
		{"null adminKey counts as unset", genesis(genesisNode(0, `null`)), fmt.Sprintf(`{"0":%q}`, keyHex(1))},
		{"equal rosterEntry and node nodeIds", `{"nodeMetadata":[{"rosterEntry":{"nodeId":"2"},"node":{"nodeId":2,"adminKey":` + ed(keyB64(1)) + `}}]}`, ""},
		{"nodeId only in node", `{"nodeMetadata":[{"node":{"nodeId":"4","adminKey":` + ed(keyB64(1)) + `}}]}`, fmt.Sprintf(`{"4":%q}`, keyHex(1))},
		{"numeric nodeId", `{"nodeMetadata":[{"rosterEntry":{"nodeId":3},"node":{"adminKey":` + ed(keyB64(1)) + `}}]}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, ValidateGenesisAdminKeys(tt.genesis, tt.adminKeys))
		})
	}
}

func TestValidateGenesisAdminKeys_Invalid(t *testing.T) {
	tests := []struct {
		name      string
		genesis   string
		adminKeys string
		wantType  *errorx.Type
		wantMsg   string
	}{
		{"node without any key", genesis(genesisNode(0, ed(keyB64(1))), genesisNode(1, ""), genesisNode(2, "")), "",
			errorx.IllegalArgument, "no admin key for node(s) 1, 2"},
		{"keys differ", genesis(genesisNode(0, ed(keyB64(1)))), fmt.Sprintf(`{"0":%q}`, keyHex(2)),
			errorx.IllegalArgument, "differ for node(s) 0"},
		{"unknown node in file", genesis(genesisNode(0, ed(keyB64(1)))), fmt.Sprintf(`{"7":%q}`, keyHex(2)),
			errorx.IllegalArgument, "node(s) 7 that are not in genesis-network.json"},
		{"bad hex in file", genesis(genesisNode(0, "")), `{"0":"zz"}`,
			errorx.IllegalFormat, "not valid hex"},
		{"0x prefix in file", genesis(genesisNode(0, "")), fmt.Sprintf(`{"0":"0x%s"}`, keyHex(1)),
			errorx.IllegalFormat, "not valid hex"},
		{"secp256k1 key in file", genesis(genesisNode(0, "")), fmt.Sprintf(`{"0":%q}`, hex.EncodeToString(bytes.Repeat([]byte{2}, 33))),
			errorx.IllegalArgument, "secp256k1"},
		{"non-numeric node ID in file", genesis(genesisNode(0, "")), fmt.Sprintf(`{"node0":%q}`, keyHex(1)),
			errorx.IllegalFormat, "invalid node ID"},
		{"file is not an object", genesis(genesisNode(0, "")), `["x"]`,
			errorx.IllegalFormat, "JSON object"},
		{"same node twice in file", genesis(genesisNode(0, "")), fmt.Sprintf(`{"0":%q,"00":%q}`, keyHex(1), keyHex(1)),
			errorx.IllegalFormat, "more than once"},
		{"non-Ed25519 genesis key", genesis(genesisNode(0, `{"ecdsaSecp256k1":"AA=="}`)), "",
			errorx.IllegalFormat, "only an Ed25519 key"},
		{"short genesis key", genesis(genesisNode(0, ed("AAAA"))), "",
			errorx.IllegalFormat, "3 bytes"},
		{"plain string genesis key (HIP-1494 example)", genesis(genesisNode(1, fmt.Sprintf("%q", keyB64(1)))), "",
			errorx.IllegalFormat, `adminKey must be an object like {"ed25519": "<base64>"}, not a string`},
		{"array genesis key", genesis(genesisNode(0, `[]`)), "",
			errorx.IllegalFormat, "not an array"},
		{"genesis not JSON", `{`, "", errorx.IllegalFormat, "not valid JSON"},
		{"genesis without nodes", `{"nodeMetadata":[]}`, "", errorx.IllegalFormat, "no nodeMetadata"},
		{"rosterEntry and node nodeIds differ", `{"nodeMetadata":[{"rosterEntry":{"nodeId":"1"},"node":{"nodeId":"2","adminKey":` + ed(keyB64(1)) + `}}]}`, "",
			errorx.IllegalFormat, "rosterEntry nodeId 1 but node nodeId 2"},
		{"same node twice in genesis", genesis(genesisNode(0, ed(keyB64(1))), genesisNode(0, ed(keyB64(1)))), "",
			errorx.IllegalFormat, "more than once"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGenesisAdminKeys(tt.genesis, tt.adminKeys)
			require.Error(t, err)
			assert.True(t, errorx.IsOfType(err, tt.wantType), "got %v", err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestValidateGenesisAdminKeys_ReportsAllProblems(t *testing.T) {
	err := ValidateGenesisAdminKeys(
		genesis(genesisNode(0, ed(keyB64(1))), genesisNode(1, "")),
		fmt.Sprintf(`{"0":%q,"9":%q}`, keyHex(2), keyHex(3)))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no admin key for node(s) 1")
	assert.Contains(t, err.Error(), "differ for node(s) 0")
	assert.Contains(t, err.Error(), "node(s) 9")
}
