// SPDX-License-Identifier: Apache-2.0

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConsensusNodeInputs_ValidateFor(t *testing.T) {
	identity := ConsensusNodeInputs{Namespace: "hiero-network-1", AccountId: "0.0.3", Weight: 500}
	noIdentity := ConsensusNodeInputs{Namespace: "hiero-network-1"}

	tests := []struct {
		name    string
		inputs  ConsensusNodeInputs
		action  ActionType
		wantErr bool
	}{
		{"install with identity", identity, ActionInstall, false},
		{"install without account id", ConsensusNodeInputs{Namespace: "ns", Weight: 500}, ActionInstall, true},
		{"install with zero weight", ConsensusNodeInputs{Namespace: "ns", AccountId: "0.0.3"}, ActionInstall, true},
		{"uninstall without identity", noIdentity, ActionUninstall, false},
		{"uninstall without namespace", ConsensusNodeInputs{}, ActionUninstall, true},
		{"install without namespace", ConsensusNodeInputs{AccountId: "0.0.3", Weight: 500}, ActionInstall, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.inputs.ValidateFor(tt.action)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Each config key needs both a CR kind and a name suffix, or install and uninstall miss a CR.
func TestConsensusConfigKeys_HaveKindAndSuffix(t *testing.T) {
	keys := ConsensusConfigKeys()
	assert.Len(t, keys, len(consensusConfigCRSuffix))
	for _, key := range keys {
		assert.NotEmpty(t, ConsensusConfigCRKind(key), "kind for %s", key)
		assert.Contains(t, consensusConfigCRSuffix, key)
	}
	assert.Empty(t, ConsensusConfigCRKind("unknown"))
}

func validCommonInputs() CommonInputs {
	mode := AllExecutionModes()[0]
	return CommonInputs{ExecutionOptions: WorkflowExecutionOptions{ExecutionMode: mode, RollbackMode: mode}}
}

// UserInputs.ValidateFor must reach the action-specific check, not only Validate.
func TestUserInputs_ValidateFor_UsesActionValidator(t *testing.T) {
	inputs := UserInputs[ConsensusNodeInputs]{
		Common: validCommonInputs(),
		Custom: ConsensusNodeInputs{Namespace: "hiero-network-1"},
	}

	assert.NoError(t, inputs.ValidateFor(ActionUninstall))
	assert.Error(t, inputs.ValidateFor(ActionInstall))
}

// Inputs without ValidateFor keep the Validate behaviour.
func TestUserInputs_ValidateFor_FallsBackToValidate(t *testing.T) {
	valid := UserInputs[BlockNodeInputs]{Common: validCommonInputs()}
	invalid := UserInputs[BlockNodeInputs]{Common: validCommonInputs(), Custom: BlockNodeInputs{Profile: "not-a-profile"}}

	assert.NoError(t, valid.ValidateFor(ActionInstall))
	assert.Error(t, invalid.ValidateFor(ActionInstall))
}
