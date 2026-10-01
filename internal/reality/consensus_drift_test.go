// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

func baselineNodeState() state.ConsensusNodeState {
	return state.ConsensusNodeState{
		Namespace: "mainnet", OrbitName: "mainnet", NodeId: 3,
		AccountId: "0.0.6", Weight: 100,
		ImageRepo: "gcr.io/hiero/consensus-node", ImageTag: "0.64.0",
		LedgerId: "0x00", ChainId: "295",
		ManagedSpec: &state.ConsensusNodeManagedSpec{
			ProvisionerDaemonEnabled: true,
			ContainerName:            "root",
			CPULimit:                 "4", CPURequest: "2",
			MemoryLimit: "16Gi", MemoryRequest: "8Gi",
			JavaHeapMin: "8g", JavaHeapMax: "12g", JavaOpts: "-XX:+UseZGC",
			UCImageRepo: "ghcr.io/hiero/solo-operator", UCImageTag: "0.8.0",
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
		},
	}
}

// liveMatching returns a live shape that matches baselineNodeState exactly.
func liveMatching() liveConsensusShape {
	return liveConsensusShape{
		ImageRepo: "gcr.io/hiero/consensus-node", ImageTag: "0.64.0",
		AccountId: "0.0.6", Weight: 100, WeightSet: true,
		LedgerId: "0x00", ChainId: "295",
		ContainerName: "root",
		CPULimit:      "4", CPURequest: "2",
		MemoryLimit: "16Gi", MemoryRequest: "8Gi",
		JavaHeapMin: "8g", JavaHeapMax: "12g", JavaOpts: "-XX:+UseZGC",
		UCImageRepo: "ghcr.io/hiero/solo-operator", UCImageTag: "0.8.0",
		ProvisionerDaemonEnabled:    true,
		ProvisionerDaemonEnabledSet: true,
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

func TestDiffConsensusManaged(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*liveConsensusShape)
		wantN    int
		wantHint string // substring expected in the (first) drift entry; "" to skip
	}{
		{"no drift", func(*liveConsensusShape) {}, 0, ""},
		{"image tag drift", func(l *liveConsensusShape) { l.ImageTag = "0.65.0" }, 1, "imageTag"},
		{"weight drift", func(l *liveConsensusShape) { l.Weight = 50 }, 1, "weight"},
		{"memory limit drift", func(l *liveConsensusShape) { l.MemoryLimit = "32Gi" }, 1, "memoryLimit"},
		{"java opts drift", func(l *liveConsensusShape) { l.JavaOpts = "-XX:+UseG1GC" }, 1, "javaOpts"},
		{"uc image tag drift", func(l *liveConsensusShape) { l.UCImageTag = "0.8.1" }, 1, "ucImageTag"},
		{
			name:  "quantity canonicalisation is not drift",
			wantN: 0,
			mutate: func(l *liveConsensusShape) {
				// API server may canonicalise; "4" CPU and "16Gi" mem are equal amounts.
				l.CPULimit = "4000m"
				l.MemoryLimit = "16384Mi"
			},
		},
		{
			name:  "empty live value is skipped (not read, not drift)",
			wantN: 0,
			mutate: func(l *liveConsensusShape) {
				l.JavaOpts = ""
				l.MemoryLimit = ""
				l.ImageTag = ""
			},
		},
		{
			name:  "multiple fields drift",
			wantN: 2,
			mutate: func(l *liveConsensusShape) {
				l.CPULimit = "8"
				l.ImageTag = "0.65.0"
			},
		},
		{
			name:     "provisionerDaemonEnabled drift",
			wantN:    1,
			wantHint: "provisionerDaemonEnabled",
			mutate:   func(l *liveConsensusShape) { l.ProvisionerDaemonEnabled = false },
		},
		{
			name:  "provisionerDaemonEnabled not set is not drift",
			wantN: 0,
			mutate: func(l *liveConsensusShape) {
				l.ProvisionerDaemonEnabledSet = false
				l.ProvisionerDaemonEnabled = false
			},
		},
		{
			name:     "imagePullSecrets default drift",
			wantN:    1,
			wantHint: "imagePullSecrets.default",
			mutate: func(l *liveConsensusShape) {
				l.ImagePullSecrets.Default = "new-default"
			},
		},
		{
			name:     "imagePullSecrets host drift",
			wantN:    1,
			wantHint: "imagePullSecrets[gcr.io]",
			mutate: func(l *liveConsensusShape) {
				l.ImagePullSecrets.ByHost["gcr.io"] = "rotated-creds"
			},
		},
		{
			name:  "imagePullSecrets not set is not drift",
			wantN: 0,
			mutate: func(l *liveConsensusShape) {
				l.ImagePullSecretsSet = false
				l.ImagePullSecrets = models.PullSecretSelector{}
			},
		},
		{
			name:     "volume type drift",
			wantN:    4,
			wantHint: "volumes[saved].type",
			mutate: func(l *liveConsensusShape) {
				l.Volumes.Volumes["saved"] = models.ConsensusVolumeSpec{Type: "pvc", Size: "500Gi", AccessMode: "ReadWriteOnce"}
			},
		},
		{
			name:     "volume path drift",
			wantN:    1,
			wantHint: "volumes[saved].path",
			mutate: func(l *liveConsensusShape) {
				l.Volumes.Volumes["saved"] = models.ConsensusVolumeSpec{Type: "hostpath", Path: "/different/path"}
			},
		},
		{
			name:  "volume pvc size quantity canonicalisation is not drift",
			wantN: 0,
			mutate: func(l *liveConsensusShape) {
				s := l.Volumes.Volumes["state"]
				s.Size = "102400Mi" // 100Gi = 102400Mi
				l.Volumes.Volumes["state"] = s
			},
		},
		{
			name:  "volumes not set is not drift",
			wantN: 0,
			mutate: func(l *liveConsensusShape) {
				l.VolumesSet = false
				l.Volumes = models.ConsensusVolumeConfig{}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			live := liveMatching()
			tc.mutate(&live)
			got := diffConsensusManaged(baselineNodeState(), live)
			if len(got) != tc.wantN {
				t.Fatalf("got %d drift entries %v, want %d", len(got), got, tc.wantN)
			}
			if tc.wantHint != "" && !contains(got[0], tc.wantHint) {
				t.Errorf("first drift entry %q does not mention %q", got[0], tc.wantHint)
			}
		})
	}
}

// TestDiffConsensusManaged_NoManagedSpecBaseline covers a pre-#1187 state file:
// with a nil ManagedSpec only identity is compared, and the managed scalars are
// skipped (no panic, no false drift from the recorded-shape fields).
func TestDiffConsensusManaged_NoManagedSpecBaseline(t *testing.T) {
	ns := baselineNodeState()
	ns.ManagedSpec = nil

	live := liveMatching()
	live.MemoryLimit = "32Gi" // managed scalar differs but must be ignored (no baseline)
	live.ImageTag = "0.65.0"  // identity differs — must still be reported

	got := diffConsensusManaged(ns, live)
	if len(got) != 1 || !contains(got[0], "imageTag") {
		t.Fatalf("want only the identity (imageTag) drift, got %v", got)
	}
}

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
		{"notaquantity", "notaquantity", false}, // equal strings, unparseable → string compare
		{"notaquantity", "4", true},             // one unparseable → string compare
	}
	for _, tc := range tests {
		if got := quantityDiffers(tc.a, tc.b); got != tc.want {
			t.Errorf("quantityDiffers(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDiffPullSecrets(t *testing.T) {
	t.Run("identical selectors produce no drift", func(t *testing.T) {
		s := models.PullSecretSelector{Default: "x", ByHost: map[string]string{"a": "1"}}
		got := diffPullSecrets(s, s)
		if len(got) != 0 {
			t.Fatalf("want 0 drift, got %v", got)
		}
	})

	t.Run("host added in live", func(t *testing.T) {
		s := models.PullSecretSelector{ByHost: map[string]string{"a": "1"}}
		l := models.PullSecretSelector{ByHost: map[string]string{"a": "1", "b": "2"}}
		got := diffPullSecrets(s, l)
		if len(got) != 1 || !contains(got[0], "imagePullSecrets[b]") {
			t.Fatalf("want 1 drift for host b, got %v", got)
		}
	})

	t.Run("host removed in live", func(t *testing.T) {
		s := models.PullSecretSelector{ByHost: map[string]string{"a": "1", "b": "2"}}
		l := models.PullSecretSelector{ByHost: map[string]string{"a": "1"}}
		got := diffPullSecrets(s, l)
		if len(got) != 1 || !contains(got[0], "imagePullSecrets[b]") {
			t.Fatalf("want 1 drift for host b, got %v", got)
		}
	})
}

func TestDiffVolumes(t *testing.T) {
	base := func() models.ConsensusVolumeConfig {
		return models.ConsensusVolumeConfig{
			Volumes: map[string]models.ConsensusVolumeSpec{
				"saved": {Type: "hostpath", Path: "/data/saved"},
				"state": {Type: "pvc", Size: "100Gi", StorageClass: "fast", AccessMode: "ReadWriteOnce"},
			},
		}
	}

	t.Run("identical configs produce no drift", func(t *testing.T) {
		got := diffVolumes(base(), base())
		if len(got) != 0 {
			t.Fatalf("want 0, got %v", got)
		}
	})

	t.Run("storage class changed", func(t *testing.T) {
		live := base()
		live.Volumes["state"] = models.ConsensusVolumeSpec{
			Type: "pvc", Size: "100Gi", StorageClass: "slow", AccessMode: "ReadWriteOnce",
		}
		got := diffVolumes(base(), live)
		if len(got) != 1 || !contains(got[0], "storageClass") {
			t.Fatalf("want 1 storageClass drift, got %v", got)
		}
	})

	t.Run("volume absent in live is skipped (not drift)", func(t *testing.T) {
		live := models.ConsensusVolumeConfig{Volumes: map[string]models.ConsensusVolumeSpec{}}
		got := diffVolumes(base(), live)
		if len(got) != 0 {
			t.Fatalf("want 0 (absent volumes skipped), got %v", got)
		}
	})

	t.Run("pvc size quantity canonicalisation is not drift", func(t *testing.T) {
		live := base()
		live.Volumes["state"] = models.ConsensusVolumeSpec{
			Type: "pvc", Size: "102400Mi", StorageClass: "fast", AccessMode: "ReadWriteOnce",
		}
		got := diffVolumes(base(), live)
		if len(got) != 0 {
			t.Fatalf("want 0 (100Gi == 102400Mi), got %v", got)
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
