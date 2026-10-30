// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConsensusPullSecretHosts_KnownHosts(t *testing.T) {
	c := &models.ConsensusNodeInputs{
		ConsensusImageRepo: "gcr.io/hedera-registry/consensus-node",
		UCImageRepo:        "ghcr.io/hashgraph/solo-operator/uc",
		ImagePullSecrets: models.PullSecretSelector{ByHost: map[string]string{
			"gcr.io":    "gcr-creds",
			"ghcr.io":   "ghcr-creds",
			"docker.io": "docker-creds",
		}},
		ConsensusImageSource: &models.ImageSource{
			Repositories: []models.ImageRepositoryRef{
				{Repository: "gcr.io/hedera-registry", ImageName: "consensus-node", ImageTag: "0.74.2"},
				{Repository: "docker.io/hashgraph", ImageName: "consensus-node", ImageTag: "0.74.2"},
			},
		},
	}
	// gcr.io + docker.io (source), ghcr.io (UC) are all known ⇒ no error.
	require.NoError(t, validateConsensusPullSecretHosts(c))
}

func TestValidateConsensusPullSecretHosts_BareDefaultAllowed(t *testing.T) {
	// A bare default has no host, so it is never rejected.
	c := &models.ConsensusNodeInputs{
		ConsensusImageRepo: "gcr.io/hedera-registry/consensus-node",
		ImagePullSecrets:   models.PullSecretSelector{Default: "private-registry-creds", HasDefault: true},
	}
	require.NoError(t, validateConsensusPullSecretHosts(c))
}

func TestValidateConsensusPullSecretHosts_UnknownHostRejected(t *testing.T) {
	c := &models.ConsensusNodeInputs{
		ConsensusImageRepo: "gcr.io/hedera-registry/consensus-node",
		UCImageRepo:        "ghcr.io/hashgraph/solo-operator/uc",
		ImagePullSecrets:   models.PullSecretSelector{ByHost: map[string]string{"typo.example.com": "creds"}},
	}
	err := validateConsensusPullSecretHosts(c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "typo.example.com")
}

func TestValidateConsensusNetworkIdentity(t *testing.T) {
	tests := []struct {
		name, ledger, chain, wantErr string
	}{
		{"hex ledger, decimal chain", "0x00", "295", ""},
		{"free-form long ledger", "ab" + strings.Repeat("cd", 31), "", ""},
		{"empty chain is allowed", "0x01", "", ""},
		{"empty ledger", "", "295", "ledger-id could not be resolved"},
		{"ledger with space", "0x 00", "295", "whitespace"},
		{"ledger with newline", "0x00\n", "295", "whitespace"},
		{"non-numeric chain", "0x00", "abc", "decimal"},
		{"negative chain", "0x00", "-1", "decimal"},
		{"hex chain", "0x00", "0x127", "decimal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConsensusNetworkIdentity(&models.ConsensusNodeInputs{LedgerId: tt.ledger, ChainId: tt.chain})
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
