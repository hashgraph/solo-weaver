// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"testing"

	"github.com/hashgraph/solo-weaver/internal/state"
)

func baselineNodeState() state.ConsensusNodeState {
	return state.ConsensusNodeState{
		Namespace: "mainnet", OrbitName: "mainnet", NodeId: 3,
		AccountId: "0.0.6", Weight: 100,
		ImageRepo: "gcr.io/hiero/consensus-node", ImageTag: "0.64.0",
		LedgerId: "0x00", ChainId: "295",
		ManagedSpec: &state.ConsensusNodeManagedSpec{
			ContainerName: "root",
			CPULimit:      "4", CPURequest: "2",
			MemoryLimit: "16Gi", MemoryRequest: "8Gi",
			JavaHeapMin: "8g", JavaHeapMax: "12g", JavaOpts: "-XX:+UseZGC",
			UCImageRepo: "ghcr.io/hiero/solo-operator", UCImageTag: "0.8.0",
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

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
