// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package drift

import (
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testScope = "mainnet/3"

// stateWith builds a state.State with a single consensus node at the given
// scope. An empty scope returns a state with no consensus nodes.
func stateWith(scope string, ns state.ConsensusNodeState) state.State {
	s := state.State{}
	if scope != "" {
		s.ConsensusNodes = map[string]state.ConsensusNodeState{scope: ns}
		s.ConsensusOrbits = map[string]state.ConsensusOrbitState{ns.OrbitName: *baselineOrbitState()}
	}
	return s
}

func baselineOrbitState() *state.ConsensusOrbitState {
	return &state.ConsensusOrbitState{ProvisionerDaemonEnabled: true, LedgerId: "0x00", ChainId: "295"}
}

func baselineManagedSpec() *state.ConsensusNodeManagedSpec {
	return &state.ConsensusNodeManagedSpec{
		ContainerName: "root",
		CPULimit:      "4",
		CPURequest:    "2",
		MemoryLimit:   "16Gi",
		MemoryRequest: "8Gi",
		JavaHeapMin:   "8g",
		JavaHeapMax:   "12g",
		JavaOpts:      "-XX:+UseZGC",
		UCImageRepo:   "ghcr.io/hiero/solo-operator",
		UCImageTag:    "0.8.0",
		ImagePullSecrets: models.PullSecretSelector{
			ByHost: map[string]string{
				"gcr.io":  "gcr-creds",
				"ghcr.io": "ghcr-creds",
			},
		},
		Volumes: models.ConsensusVolumeConfig{
			Volumes: map[string]models.ConsensusVolumeSpec{
				"upgrade": {Type: "emptydir"},
				"logs":    {Type: "emptydir"},
				"stats":   {Type: "emptydir"},
				"saved":   {Type: "hostpath", Path: "/opt/hgcapp/data/saved"},
				"state":   {Type: "pvc", Size: "100Gi", StorageClass: "fast", AccessMode: "ReadWriteOnce"},
				"blocks":  {Type: "emptydir"},
				"records": {Type: "emptydir"},
				"events":  {Type: "emptydir"},
			},
		},
	}
}

func matchingObservedShape() *state.ConsensusNodeObservedShape {
	return &state.ConsensusNodeObservedShape{
		ContainerName: "root",
		CPULimit:      "4",
		CPURequest:    "2",
		MemoryLimit:   "16Gi",
		MemoryRequest: "8Gi",
		JavaHeapMin:   "8g",
		JavaHeapMax:   "12g",
		JavaOpts:      "-XX:+UseZGC",
		UCImageRepo:   "ghcr.io/hiero/solo-operator",
		UCImageTag:    "0.8.0",
		Orbit:         baselineOrbitState(),
		ImagePullSecrets: models.PullSecretSelector{
			ByHost: map[string]string{
				"gcr.io":  "gcr-creds",
				"ghcr.io": "ghcr-creds",
			},
		},
		ImagePullSecretsSet: true,
		Volumes: models.ConsensusVolumeConfig{
			Volumes: map[string]models.ConsensusVolumeSpec{
				"upgrade": {Type: "emptydir"},
				"logs":    {Type: "emptydir"},
				"stats":   {Type: "emptydir"},
				"saved":   {Type: "hostpath", Path: "/opt/hgcapp/data/saved"},
				"state":   {Type: "pvc", Size: "100Gi", StorageClass: "fast", AccessMode: "ReadWriteOnce"},
				"blocks":  {Type: "emptydir"},
				"records": {Type: "emptydir"},
				"events":  {Type: "emptydir"},
			},
		},
		VolumesSet: true,
	}
}

func baselineConsensusNodeState() state.ConsensusNodeState {
	return state.ConsensusNodeState{
		Namespace: "mainnet", OrbitName: "mainnet", NodeId: 3,
		AccountId: "0.0.6", Weight: 100,
		ImageRepo: "gcr.io/hiero/consensus-node", ImageTag: "0.64.0",
		LedgerId: "0x00", ChainId: "295",
		ManagedSpec: baselineManagedSpec(),
	}
}

func liveConsensusNodeState() state.ConsensusNodeState {
	ns := baselineConsensusNodeState()
	ns.ObservedShape = matchingObservedShape()
	return ns
}

// ── Identity drift (baseline vs live) ────────────────────────────────────

func TestConsensusNode_IdentityDrift(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(live *state.ConsensusNodeState)
		wantN    int
		wantHint string
	}{
		{"no drift", func(*state.ConsensusNodeState) {}, 0, ""},
		{"image tag drift", func(ns *state.ConsensusNodeState) { ns.ImageTag = "0.65.0" }, 1, "imageTag"},
		{"image repo drift", func(ns *state.ConsensusNodeState) { ns.ImageRepo = "docker.io/hiero/consensus-node" }, 1, "imageRepo"},
		{"weight drift", func(ns *state.ConsensusNodeState) { ns.Weight = 50 }, 1, "weight"},
		{"accountId drift", func(ns *state.ConsensusNodeState) { ns.AccountId = "0.0.99" }, 1, "accountId"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseline := stateWith(testScope, baselineConsensusNodeState())
			live := liveConsensusNodeState()
			tc.mutate(&live)
			liveState := stateWith(testScope, live)

			got := ConsensusNode(baseline, liveState)
			require.Len(t, got, tc.wantN)
			if tc.wantHint != "" {
				assert.Contains(t, got[0].Field, tc.wantHint)
				assert.Contains(t, got[0].Component, "consensus["+testScope+"]")
			}
		})
	}
}

func TestConsensusNode_NewNodeIsNotReportedAsDrift(t *testing.T) {
	baseline := stateWith("", state.ConsensusNodeState{}) // empty
	live := liveConsensusNodeState()
	live.ImageTag = "0.65.0"
	liveState := stateWith(testScope, live)

	got := ConsensusNode(baseline, liveState)
	assert.Empty(t, got, "new node that wasn't in baseline should produce no identity drift")
}

// ── Managed-spec drift (ManagedSpec vs ObservedShape) ────────────────────

func TestConsensusNode_ManagedSpecDrift(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(o *state.ConsensusNodeObservedShape)
		wantN    int
		wantHint string
	}{
		{"no drift", func(*state.ConsensusNodeObservedShape) {}, 0, ""},
		{"memory limit drift", func(o *state.ConsensusNodeObservedShape) { o.MemoryLimit = "32Gi" }, 1, "memoryLimit"},
		{"java opts drift", func(o *state.ConsensusNodeObservedShape) { o.JavaOpts = "-XX:+UseG1GC" }, 1, "javaOpts"},
		{"uc image tag drift", func(o *state.ConsensusNodeObservedShape) { o.UCImageTag = "0.8.1" }, 1, "ucImageTag"},
		{"container name drift", func(o *state.ConsensusNodeObservedShape) { o.ContainerName = "other" }, 1, "containerName"},
		{
			name:  "quantity canonicalisation is not drift",
			wantN: 0,
			mutate: func(o *state.ConsensusNodeObservedShape) {
				o.CPULimit = "4000m"
				o.MemoryLimit = "16384Mi"
			},
		},
		{
			name:  "field deleted from the live capsule is drift",
			wantN: 2,
			mutate: func(o *state.ConsensusNodeObservedShape) {
				o.JavaOpts = ""
				o.MemoryLimit = ""
			},
		},
		{
			name:  "multiple fields drift",
			wantN: 2,
			mutate: func(o *state.ConsensusNodeObservedShape) {
				o.CPULimit = "8"
				o.UCImageTag = "0.9.0"
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseline := stateWith(testScope, baselineConsensusNodeState())
			live := liveConsensusNodeState()
			tc.mutate(live.ObservedShape)
			liveState := stateWith(testScope, live)

			got := ConsensusNode(baseline, liveState)
			require.Len(t, got, tc.wantN)
			if tc.wantHint != "" {
				assert.Contains(t, got[0].Field, tc.wantHint)
			}
		})
	}
}

func TestConsensusNode_NoManagedSpecSkipsManagedComparison(t *testing.T) {
	baseline := stateWith(testScope, baselineConsensusNodeState())
	live := liveConsensusNodeState()
	live.ManagedSpec = nil
	live.ImageTag = "0.65.0" // identity drift should still be reported
	liveState := stateWith(testScope, live)

	got := ConsensusNode(baseline, liveState)
	require.Len(t, got, 1)
	assert.Equal(t, "imageTag", got[0].Field)
}

func TestConsensusNode_NoObservedShapeSkipsManagedComparison(t *testing.T) {
	baseline := stateWith(testScope, baselineConsensusNodeState())
	live := liveConsensusNodeState()
	live.ObservedShape = nil
	liveState := stateWith(testScope, live)

	got := ConsensusNode(baseline, liveState)
	assert.Empty(t, got, "no observed shape means cluster was unreadable; no managed-spec drift")
}

// ── Pull secrets ─────────────────────────────────────────────────────────

func TestConsensusNode_PullSecretsDrift(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(o *state.ConsensusNodeObservedShape)
		wantN    int
		wantHint string
	}{
		{
			name:     "secret removed from live host",
			wantN:    1,
			wantHint: "imagePullSecrets[gcr.io]",
			mutate:   func(o *state.ConsensusNodeObservedShape) { o.ImagePullSecrets.ByHost["gcr.io"] = "" },
		},
		{
			name:     "host secret changed",
			wantN:    1,
			wantHint: "imagePullSecrets[gcr.io]",
			mutate:   func(o *state.ConsensusNodeObservedShape) { o.ImagePullSecrets.ByHost["gcr.io"] = "rotated-creds" },
		},
		{
			name:  "not set skips comparison",
			wantN: 0,
			mutate: func(o *state.ConsensusNodeObservedShape) {
				o.ImagePullSecretsSet = false
				o.ImagePullSecrets = models.PullSecretSelector{}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseline := stateWith(testScope, baselineConsensusNodeState())
			live := liveConsensusNodeState()
			tc.mutate(live.ObservedShape)
			liveState := stateWith(testScope, live)

			got := ConsensusNode(baseline, liveState)
			require.Len(t, got, tc.wantN)
			if tc.wantHint != "" {
				assert.Contains(t, got[0].Field, tc.wantHint)
			}
		})
	}
}

func TestConsensusNode_PullSecretsDefaultResolvedPerHost(t *testing.T) {
	spec := models.PullSecretSelector{Default: "creds", HasDefault: true}
	live := liveConsensusNodeState()
	live.ManagedSpec.ImagePullSecrets = spec
	live.ObservedShape.ImagePullSecrets = models.PullSecretSelector{
		ByHost: map[string]string{"gcr.io": "creds", "ghcr.io": "creds"},
	}
	baseline := stateWith(testScope, baselineConsensusNodeState())
	assert.Empty(t, ConsensusNode(baseline, stateWith(testScope, live)))

	live.ObservedShape.ImagePullSecrets.ByHost["ghcr.io"] = ""
	got := ConsensusNode(baseline, stateWith(testScope, live))
	require.Len(t, got, 1)
	assert.Equal(t, "imagePullSecrets[ghcr.io]", got[0].Field)
}

func TestConsensusNode_HostPathOwnerDrift(t *testing.T) {
	baseline := stateWith(testScope, baselineConsensusNodeState())
	live := liveConsensusNodeState()
	live.ManagedSpec.HostPathUID = 3000
	live.ManagedSpec.HostPathGID = 3000
	live.ObservedShape.HostPathOwners = map[string]state.HostPathOwner{
		"saved": {UID: 3000, GID: 3000},
	}
	assert.Empty(t, ConsensusNode(baseline, stateWith(testScope, live)))

	live.ObservedShape.HostPathOwners["saved"] = state.HostPathOwner{UID: 0, GID: 3000}
	got := ConsensusNode(baseline, stateWith(testScope, live))
	require.Len(t, got, 1)
	assert.Equal(t, "hostPathUid[saved]", got[0].Field)
	assert.Equal(t, "3000", got[0].Persisted)
	assert.Equal(t, "0", got[0].Live)
}

// ── Volumes ──────────────────────────────────────────────────────────────

func TestConsensusNode_VolumesDrift(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(o *state.ConsensusNodeObservedShape)
		wantN    int
		wantHint string
	}{
		{
			name:     "type changed",
			wantN:    4, // type + path diff + size + accessMode (saved hostpath→pvc changes several fields)
			wantHint: "volumes[saved].type",
			mutate: func(o *state.ConsensusNodeObservedShape) {
				o.Volumes.Volumes["saved"] = models.ConsensusVolumeSpec{Type: "pvc", Size: "500Gi", AccessMode: "ReadWriteOnce"}
			},
		},
		{
			name:     "path changed",
			wantN:    1,
			wantHint: "volumes[saved].path",
			mutate: func(o *state.ConsensusNodeObservedShape) {
				o.Volumes.Volumes["saved"] = models.ConsensusVolumeSpec{Type: "hostpath", Path: "/different/path"}
			},
		},
		{
			name:  "pvc size quantity canonicalisation is not drift",
			wantN: 0,
			mutate: func(o *state.ConsensusNodeObservedShape) {
				s := o.Volumes.Volumes["state"]
				s.Size = "102400Mi" // 100Gi = 102400Mi
				o.Volumes.Volumes["state"] = s
			},
		},
		{
			name:  "not set skips comparison",
			wantN: 0,
			mutate: func(o *state.ConsensusNodeObservedShape) {
				o.VolumesSet = false
				o.Volumes = models.ConsensusVolumeConfig{}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			baseline := stateWith(testScope, baselineConsensusNodeState())
			live := liveConsensusNodeState()
			tc.mutate(live.ObservedShape)
			liveState := stateWith(testScope, live)

			got := ConsensusNode(baseline, liveState)
			require.Len(t, got, tc.wantN)
			if tc.wantHint != "" {
				assert.Contains(t, got[0].Field, tc.wantHint)
			}
		})
	}
}

// ── Idempotency ──────────────────────────────────────────────────────────

func TestConsensusNode_NoConsensusNodesProducesNoDrift(t *testing.T) {
	empty := state.State{}
	got := ConsensusNode(empty, empty)
	assert.Empty(t, got, "no consensus nodes means no drift, ever")
}

func TestConsensusNode_NilMapsProduceNoDrift(t *testing.T) {
	// ConsensusNodes is nil on both sides (the zero value)
	var baseline, live state.State
	got := ConsensusNode(baseline, live)
	assert.Empty(t, got)
}

// ── Helpers ──────────────────────────────────────────────────────────────

func TestQuantityDiffers(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"4", "4", false},
		{"4", "4000m", false},
		{"16Gi", "16384Mi", false},
		{"4", "8", true},
		{"16Gi", "32Gi", true},
		{"notaquantity", "notaquantity", false},
		{"notaquantity", "4", true},
	}
	for _, tc := range tests {
		if got := quantityDiffers(tc.a, tc.b); got != tc.want {
			t.Errorf("quantityDiffers(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestConsensusNode_WarningFormatMatchesFramework(t *testing.T) {
	baseline := stateWith(testScope, baselineConsensusNodeState())
	live := liveConsensusNodeState()
	live.ImageTag = "0.65.0"
	liveState := stateWith(testScope, live)

	got := ConsensusNode(baseline, liveState)
	require.Len(t, got, 1)
	// Verify it renders through Change.String() in the framework format
	s := got[0].String()
	assert.True(t, strings.Contains(s, "differs from persisted state"), "warning should use framework format, got: %s", s)
	assert.True(t, strings.Contains(s, "consensus["+testScope+"]"), "warning should identify the node, got: %s", s)
}

// ── Orbit-level drift ────────────────────────────────────────────────────

func TestConsensusNode_OrbitDriftReportedOncePerOrbit(t *testing.T) {
	live := stateWith(testScope, liveConsensusNodeState())
	second := liveConsensusNodeState()
	second.NodeId = 4
	live.ConsensusNodes["mainnet/4"] = second
	for k, ns := range live.ConsensusNodes {
		ns.ObservedShape.Orbit.ProvisionerDaemonEnabled = false
		live.ConsensusNodes[k] = ns
	}

	got := ConsensusNode(stateWith(testScope, baselineConsensusNodeState()), live)
	require.Len(t, got, 1, "two nodes on one Orbit must yield a single Orbit change")
	assert.Equal(t, "consensus-orbit[mainnet]", got[0].Component)
	assert.Equal(t, "provisionerDaemonEnabled", got[0].Field)
	assert.Equal(t, "true", got[0].Persisted)
	assert.Equal(t, "false", got[0].Live)
}

func TestConsensusNode_NoOrbitBaselineSkipsOrbitComparison(t *testing.T) {
	live := stateWith(testScope, liveConsensusNodeState())
	live.ConsensusOrbits = nil
	for k, ns := range live.ConsensusNodes {
		ns.ObservedShape.Orbit.ProvisionerDaemonEnabled = false
		live.ConsensusNodes[k] = ns
	}

	assert.Empty(t, ConsensusNode(stateWith(testScope, baselineConsensusNodeState()), live))
}

func TestConsensusNode_OrbitFieldDrift(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(o *state.ConsensusOrbitState)
		wantField string
	}{
		{"daemon flag", func(o *state.ConsensusOrbitState) { o.ProvisionerDaemonEnabled = false }, "provisionerDaemonEnabled"},
		{"ledgerId", func(o *state.ConsensusOrbitState) { o.LedgerId = "0x01" }, "ledgerId"},
		{"chainId", func(o *state.ConsensusOrbitState) { o.ChainId = "296" }, "chainId"},
		{"realmId", func(o *state.ConsensusOrbitState) { o.RealmId = 1 }, "realmId"},
		{"shardId", func(o *state.ConsensusOrbitState) { o.ShardId = 2 }, "shardId"},
		{"provisioningModel", func(o *state.ConsensusOrbitState) { o.ProvisioningModel = "InitContainerJava" }, "provisioningModel"},
		{"requireDigestOnDeploy", func(o *state.ConsensusOrbitState) { o.RequireDigestOnDeploy = true }, "requireDigestOnDeploy"},
		{"deploymentModel", func(o *state.ConsensusOrbitState) { o.DeploymentModel = "MultipleKubernetesClusters" }, "deploymentModel"},
		{"external address added", func(o *state.ConsensusOrbitState) {
			o.ExternalAddresses = []state.OrbitExternalAddress{{NodeId: 7, Digest: "abc"}}
		}, "externalAddresses[7]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			live := stateWith(testScope, liveConsensusNodeState())
			for k, ns := range live.ConsensusNodes {
				tc.mutate(ns.ObservedShape.Orbit)
				live.ConsensusNodes[k] = ns
			}
			got := ConsensusNode(stateWith(testScope, baselineConsensusNodeState()), live)
			require.Len(t, got, 1)
			assert.Equal(t, "consensus-orbit[mainnet]", got[0].Component)
			assert.Equal(t, tc.wantField, got[0].Field)
		})
	}
}

func TestConsensusNode_OrbitUnreadIsNotDrift(t *testing.T) {
	live := stateWith(testScope, liveConsensusNodeState())
	for k, ns := range live.ConsensusNodes {
		ns.ObservedShape.Orbit = nil
		live.ConsensusNodes[k] = ns
	}
	assert.Empty(t, ConsensusNode(stateWith(testScope, baselineConsensusNodeState()), live))
}

func TestConsensusNode_EmptyChainIdEqualsZero(t *testing.T) {
	baseline := stateWith(testScope, baselineConsensusNodeState())
	for k, ns := range baseline.ConsensusOrbits {
		ns.ChainId = ""
		baseline.ConsensusOrbits[k] = ns
	}
	live := stateWith(testScope, liveConsensusNodeState())
	for k, ns := range live.ConsensusNodes {
		ns.ObservedShape.Orbit.ChainId = "0"
		live.ConsensusNodes[k] = ns
	}
	live.ConsensusOrbits = baseline.ConsensusOrbits
	assert.Empty(t, ConsensusNode(baseline, live))
}
