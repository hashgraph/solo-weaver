// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"errors"
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

// startPolicyClient serves only the capsule's spec.startPolicy and the CRD check.
type startPolicyClient struct {
	keyCluster
	live      string
	err       error
	reads     int
	crdAbsent bool
	crdErr    error
}

func (c *startPolicyClient) GetResourceNestedString(_ context.Context, _, _, _, _ string, _ ...string) (string, error) {
	c.reads++
	return c.live, c.err
}

func (c *startPolicyClient) CRDExists(_ context.Context, _ string) (bool, error) {
	return !c.crdAbsent, c.crdErr
}

func TestResolveStartPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, flag, live, want string
		wantReads              int
	}{
		{"flag wins, no read", "Auto", "Manual", "Auto", 0},
		{"empty keeps live Manual", "", "Manual", "Manual", 1},
		{"empty on a new node gives Auto", "", "", "Auto", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &startPolicyClient{live: tc.live}
			in := &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns", StartPolicy: tc.flag}
			require.NoError(t, resolveStartPolicy(context.Background(), c, in))
			assert.Equal(t, tc.want, in.StartPolicy)
			assert.Equal(t, tc.wantReads, c.reads)
		})
	}

	t.Run("read error fails, so the policy is not reset", func(t *testing.T) {
		c := &startPolicyClient{err: errors.New("forbidden")}
		in := &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns"}
		require.Error(t, resolveStartPolicy(context.Background(), c, in))
		assert.Empty(t, in.StartPolicy)
	})

	t.Run("read error with a failed CRD check fails", func(t *testing.T) {
		c := &startPolicyClient{err: errors.New("forbidden"), crdErr: errors.New("forbidden")}
		in := &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns"}
		require.Error(t, resolveStartPolicy(context.Background(), c, in))
		assert.Empty(t, in.StartPolicy)
	})

	t.Run("missing CRD gives Auto, so the operator precheck reports it", func(t *testing.T) {
		c := &startPolicyClient{err: errors.New("no matches for kind"), crdAbsent: true}
		in := &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns"}
		require.NoError(t, resolveStartPolicy(context.Background(), c, in))
		assert.Equal(t, models.StartPolicyAuto, in.StartPolicy)
	})
}
