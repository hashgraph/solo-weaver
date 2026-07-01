// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

func TestBlockNodeProvider_LocalProfile(t *testing.T) {
	p := &blockNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeBlock, Profile: models.ProfileLocal}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 3 {
		t.Errorf("expected 3 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 1 {
		t.Errorf("expected 1 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 1 {
		t.Errorf("expected 1 GB storage, got %d", reqs.MinStorageGB)
	}
}

// TestBlockNodeProvider_TestnetLFH verifies testnet LFH (default / no preset): n2d-standard-16, 5 TB.
func TestBlockNodeProvider_TestnetLFH(t *testing.T) {
	p := &blockNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeBlock, Profile: models.ProfileTestnet}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 16 {
		t.Errorf("expected 16 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 64 {
		t.Errorf("expected 64 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 5000 {
		t.Errorf("expected 5000 GB storage, got %d", reqs.MinStorageGB)
	}
}

// TestBlockNodeProvider_TestnetRFH verifies testnet RFH (tier1-rfh preset): c3d-standard-8, 150 GB.
func TestBlockNodeProvider_TestnetRFH(t *testing.T) {
	p := &blockNodeProvider{}
	spec := DeploymentSpec{
		NodeType: models.NodeTypeBlock,
		Profile:  models.ProfileTestnet,
		Options:  map[string]any{"preset": "tier1-rfh"},
	}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 8 {
		t.Errorf("expected 8 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 32 {
		t.Errorf("expected 32 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 150 {
		t.Errorf("expected 150 GB storage, got %d", reqs.MinStorageGB)
	}
}

// TestBlockNodeProvider_PerfnetLFH verifies perfnet uses the same shape as testnet LFH.
func TestBlockNodeProvider_PerfnetLFH(t *testing.T) {
	p := &blockNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeBlock, Profile: models.ProfilePerfnet}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 16 {
		t.Errorf("expected 16 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinStorageGB != 5000 {
		t.Errorf("expected 5000 GB storage, got %d", reqs.MinStorageGB)
	}
}

// TestBlockNodeProvider_PreviewnetLFH verifies previewnet LFH (default / no preset): n2d-standard-16, 3 TB.
func TestBlockNodeProvider_PreviewnetLFH(t *testing.T) {
	p := &blockNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeBlock, Profile: models.ProfilePreviewnet}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 16 {
		t.Errorf("expected 16 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 64 {
		t.Errorf("expected 64 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 3000 {
		t.Errorf("expected 3000 GB storage, got %d", reqs.MinStorageGB)
	}
}

// TestBlockNodeProvider_PreviewnetRFH verifies previewnet RFH (tier1-rfh preset): c3d-standard-8, 150 GB.
func TestBlockNodeProvider_PreviewnetRFH(t *testing.T) {
	p := &blockNodeProvider{}
	spec := DeploymentSpec{
		NodeType: models.NodeTypeBlock,
		Profile:  models.ProfilePreviewnet,
		Options:  map[string]any{"preset": "tier1-rfh"},
	}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 8 {
		t.Errorf("expected 8 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 32 {
		t.Errorf("expected 32 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 150 {
		t.Errorf("expected 150 GB storage, got %d", reqs.MinStorageGB)
	}
}

// TestBlockNodeProvider_MainnetCloud verifies mainnet cloud RFH minimum: n2d-highmem-32, 150 GB.
// Mainnet LFH runs on bare metal (lfh_count=0) — no rule fires for that case (no-op check).
func TestBlockNodeProvider_MainnetCloud(t *testing.T) {
	p := &blockNodeProvider{}
	spec := DeploymentSpec{
		NodeType: models.NodeTypeBlock,
		Profile:  models.ProfileMainnet,
		Options:  map[string]any{"preset": "tier1-rfh"},
	}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 32 {
		t.Errorf("expected 32 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 256 {
		t.Errorf("expected 256 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 150 {
		t.Errorf("expected 150 GB storage, got %d", reqs.MinStorageGB)
	}
}

// TestBlockNodeProvider_MainnetLFH verifies that mainnet with no preset (LFH bare metal)
// fires no rule — Reduce returns zero requirements, so all hardware checks trivially pass.
func TestBlockNodeProvider_MainnetLFH(t *testing.T) {
	p := &blockNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeBlock, Profile: models.ProfileMainnet}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 0 {
		t.Errorf("expected 0 CPU cores (no-op check), got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 0 {
		t.Errorf("expected 0 GB memory (no-op check), got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 0 {
		t.Errorf("expected 0 GB storage (no-op check), got %d", reqs.MinStorageGB)
	}
}

func TestBlockNodeProvider_SupportedOS(t *testing.T) {
	p := &blockNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeBlock, Profile: models.ProfileLocal}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs.MinSupportedOS) == 0 {
		t.Error("expected at least one supported OS")
	}
}

func TestBlockNodeProvider_ComputeWithWhy(t *testing.T) {
	p := &blockNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeBlock, Profile: models.ProfileTestnet}
	_, why, err := p.ComputeWithWhy(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if why["cpu"] == "" {
		t.Error("expected non-empty Why for cpu")
	}
	if why["memory"] == "" {
		t.Error("expected non-empty Why for memory")
	}
	if why["storage"] == "" {
		t.Error("expected non-empty Why for storage")
	}
}

// TestReduceMaxSemantics verifies that CPU and memory use Max reduction.
func TestReduceMaxSemantics(t *testing.T) {
	rules := []Rule{
		{
			When: always(),
			Then: Contribution{CpuCores: 4, MemoryGB: 8, Why: "base"},
		},
		{
			When: always(),
			Then: Contribution{CpuCores: 8, MemoryGB: 16, Why: "higher"},
		},
	}
	spec := DeploymentSpec{}
	reqs, why, err := Reduce(rules, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 8 {
		t.Errorf("expected Max CPU 8, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 16 {
		t.Errorf("expected Max memory 16, got %d", reqs.MinMemoryGB)
	}
	if why["cpu"] != "higher" {
		t.Errorf("expected Why[cpu]='higher', got %q", why["cpu"])
	}
}

// TestReduceSumSemantics verifies that storage uses Sum reduction.
func TestReduceSumSemantics(t *testing.T) {
	rules := []Rule{
		{
			When: always(),
			Then: Contribution{StorageGB: 100, Why: "base storage"},
		},
		{
			When: always(),
			Then: Contribution{StorageGB: 50, Why: "extra storage"},
		},
	}
	spec := DeploymentSpec{}
	reqs, why, err := Reduce(rules, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinStorageGB != 150 {
		t.Errorf("expected Sum storage 150, got %d", reqs.MinStorageGB)
	}
	if why["storage"] == "" {
		t.Error("expected non-empty Why for storage")
	}
}

// TestNotPredicate verifies the not combinator inverts a predicate.
func TestNotPredicate(t *testing.T) {
	rfh := presetPredicate("tier1-rfh")
	notRFH := not(rfh)

	noPreset := DeploymentSpec{}
	if !notRFH(noPreset) {
		t.Error("not(rfh) should be true when no preset is set")
	}

	withRFH := DeploymentSpec{Options: map[string]any{"preset": "tier1-rfh"}}
	if notRFH(withRFH) {
		t.Error("not(rfh) should be false when preset is tier1-rfh")
	}
}
