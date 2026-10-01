// SPDX-License-Identifier: Apache-2.0

package state

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	htime "helm.sh/helm/v3/pkg/time"
)

// sampleManagedSpec returns a fully-populated managed spec for tests.
func sampleManagedSpec() *ConsensusNodeManagedSpec {
	return &ConsensusNodeManagedSpec{
		ProvisionerDaemonEnabled: true,
		ContainerName:            "root",
		CPULimit:                 "4",
		CPURequest:               "2",
		MemoryLimit:              "16Gi",
		MemoryRequest:            "8Gi",
		JavaHeapMin:              "8g",
		JavaHeapMax:              "12g",
		JavaOpts:                 "-XX:+UseZGC",
		UCImageRepo:              "ghcr.io/hiero/solo-operator",
		UCImageTag:               "0.8.0",
		ImagePullSecrets: models.PullSecretSelector{
			HasDefault: true,
			Default:    "default-creds",
			ByHost:     map[string]string{"ghcr.io": "ghcr-creds", "gcr.io": "gcr-creds"},
		},
		Volumes: models.ConsensusVolumeConfig{
			Volumes: map[string]models.ConsensusVolumeSpec{
				"saved": {Type: "hostpath", Path: "/opt/hgcapp/data/saved"},
				"state": {Type: "pvc", Size: "100Gi", StorageClass: "fast", AccessMode: "ReadWriteOnce"},
			},
		},
		HostPathUID: 2000,
		HostPathGID: 2000,
	}
}

func TestSpecHash(t *testing.T) {
	t.Run("nil is empty", func(t *testing.T) {
		assert.Equal(t, "", SpecHash(nil))
	})

	t.Run("deterministic across calls", func(t *testing.T) {
		m := sampleManagedSpec()
		assert.Equal(t, SpecHash(m), SpecHash(m))
	})

	t.Run("independent of map insertion order", func(t *testing.T) {
		// json.Marshal sorts map keys, so two specs whose maps were built in
		// different orders must hash identically.
		a := sampleManagedSpec()
		b := sampleManagedSpec()
		b.ImagePullSecrets.ByHost = map[string]string{"gcr.io": "gcr-creds", "ghcr.io": "ghcr-creds"}
		assert.Equal(t, SpecHash(a), SpecHash(b))
	})
}

func TestConsensusNodeManagedSpec_Hash(t *testing.T) {
	t.Run("nil receiver is empty", func(t *testing.T) {
		var m *ConsensusNodeManagedSpec
		assert.Equal(t, "", m.Hash())
	})

	t.Run("equal shapes hash equal", func(t *testing.T) {
		assert.Equal(t, sampleManagedSpec().Hash(), sampleManagedSpec().Hash())
	})

	t.Run("any field change flips the hash", func(t *testing.T) {
		base := sampleManagedSpec().Hash()

		m1 := sampleManagedSpec()
		m1.MemoryLimit = "32Gi"
		assert.NotEqual(t, base, m1.Hash(), "scalar change must change hash")

		m2 := sampleManagedSpec()
		m2.Volumes.Volumes["saved"] = models.ConsensusVolumeSpec{Type: "emptydir"}
		assert.NotEqual(t, base, m2.Hash(), "volume-backing change must change hash")

		m3 := sampleManagedSpec()
		m3.ImagePullSecrets.ByHost["ghcr.io"] = "rotated-creds"
		assert.NotEqual(t, base, m3.Hash(), "pull-secret change must change hash")
	})
}

func TestConsensusNodeManagedSpec_Equal(t *testing.T) {
	t.Run("nil and nil are equal", func(t *testing.T) {
		var a, b *ConsensusNodeManagedSpec
		assert.True(t, a.Equal(b))
	})

	t.Run("nil and non-nil differ", func(t *testing.T) {
		var a *ConsensusNodeManagedSpec
		assert.False(t, a.Equal(sampleManagedSpec()))
		assert.False(t, sampleManagedSpec().Equal(a))
	})

	t.Run("empty-but-non-nil differs from nil", func(t *testing.T) {
		// nil = "never recorded"; {} = "recorded, all defaults" — not the same.
		var a *ConsensusNodeManagedSpec
		assert.False(t, (&ConsensusNodeManagedSpec{}).Equal(a))
	})

	t.Run("same shape equal, changed shape not", func(t *testing.T) {
		assert.True(t, sampleManagedSpec().Equal(sampleManagedSpec()))
		changed := sampleManagedSpec()
		changed.CPULimit = "8"
		assert.False(t, sampleManagedSpec().Equal(changed))
	})
}

func TestConsensusNodeState_Equal(t *testing.T) {
	base := func() ConsensusNodeState {
		return ConsensusNodeState{
			Namespace: "mainnet", OrbitName: "mainnet", NodeId: 3,
			AccountId: "0.0.6", Weight: 100,
			ImageRepo: "gcr.io/hiero/consensus-node", ImageTag: "0.64.0",
			LedgerId: "0x00", ChainId: "295",
			GrpcTlsSecret: "node3-grpc-tls-keys", SigningSecret: "node3-gossip-keys",
			ConfigHashes: map[string]ConfigHashEntry{
				"settings": {Hash: "abc", Source: models.ConfigSourcePackage, LastUpdate: htime.Now()},
			},
			ManagedSpec:     sampleManagedSpec(),
			ManagedSpecHash: sampleManagedSpec().Hash(),
			LastSync:        htime.Now(),
		}
	}

	t.Run("ignores LastSync and DeploymentPkg", func(t *testing.T) {
		a := base()
		b := base()
		b.LastSync = htime.Time{}
		b.DeploymentPkg = "/some/other/path"
		assert.True(t, (&a).Equal(b))
	})

	t.Run("detects identity change", func(t *testing.T) {
		a := base()
		b := base()
		b.Weight = 50
		assert.False(t, (&a).Equal(b))
	})

	t.Run("detects config-hash content change but ignores LastUpdate", func(t *testing.T) {
		a := base()

		sameButLaterTime := base()
		e := sameButLaterTime.ConfigHashes["settings"]
		e.LastUpdate = htime.Time{}
		sameButLaterTime.ConfigHashes["settings"] = e
		assert.True(t, (&a).Equal(sameButLaterTime), "LastUpdate must not count as drift")

		changedContent := base()
		e2 := changedContent.ConfigHashes["settings"]
		e2.Hash = "def"
		changedContent.ConfigHashes["settings"] = e2
		assert.False(t, (&a).Equal(changedContent))
	})

	t.Run("detects managed-spec change", func(t *testing.T) {
		a := base()
		b := base()
		changed := sampleManagedSpec()
		changed.MemoryLimit = "32Gi"
		b.ManagedSpec = changed
		assert.False(t, (&a).Equal(b))
	})

	t.Run("detects nil-vs-set managed spec", func(t *testing.T) {
		a := base()
		b := base()
		b.ManagedSpec = nil
		assert.False(t, (&a).Equal(b))
	})
}

// TestConsensusNodeState_YAMLMigrationSafe verifies the new fields are additive:
// a state file written before ManagedSpec existed loads with a nil ManagedSpec
// and no error, and a round-trip preserves the recorded shape.
func TestConsensusNodeState_YAMLMigrationSafe(t *testing.T) {
	t.Run("old file without managedSpec loads as nil", func(t *testing.T) {
		old := []byte("" +
			"namespace: mainnet\n" +
			"orbitName: mainnet\n" +
			"nodeId: 3\n" +
			"imageRepo: gcr.io/hiero/consensus-node\n" +
			"imageTag: 0.64.0\n")
		var ns ConsensusNodeState
		require.NoError(t, yaml.Unmarshal(old, &ns))
		assert.Nil(t, ns.ManagedSpec)
		assert.Equal(t, "", ns.ManagedSpecHash)
		assert.Equal(t, int64(3), ns.NodeId)
	})

	t.Run("round-trip preserves the managed shape", func(t *testing.T) {
		in := ConsensusNodeState{
			Namespace: "mainnet", OrbitName: "mainnet", NodeId: 3,
			ManagedSpec:     sampleManagedSpec(),
			ManagedSpecHash: sampleManagedSpec().Hash(),
		}
		b, err := yaml.Marshal(in)
		require.NoError(t, err)

		var out ConsensusNodeState
		require.NoError(t, yaml.Unmarshal(b, &out))
		require.NotNil(t, out.ManagedSpec)
		assert.Equal(t, in.ManagedSpec.Hash(), out.ManagedSpec.Hash())
		assert.True(t, (&in).Equal(out))
	})
}
