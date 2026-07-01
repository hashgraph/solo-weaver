// SPDX-License-Identifier: Apache-2.0

package hardware

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

func TestConsensusNodeProvider_LocalProfile(t *testing.T) {
	p := &consensusNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeConsensus, Profile: models.ProfileLocal}
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

func TestConsensusNodeProvider_TestnetProfile(t *testing.T) {
	p := &consensusNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeConsensus, Profile: models.ProfileTestnet}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 16 {
		t.Errorf("expected 16 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 32 {
		t.Errorf("expected 32 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 1000 {
		t.Errorf("expected 1000 GB storage, got %d", reqs.MinStorageGB)
	}
}

func TestConsensusNodeProvider_PerfnetProfile(t *testing.T) {
	p := &consensusNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeConsensus, Profile: models.ProfilePerfnet}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 16 {
		t.Errorf("expected 16 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 32 {
		t.Errorf("expected 32 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 1000 {
		t.Errorf("expected 1000 GB storage, got %d", reqs.MinStorageGB)
	}
}

func TestConsensusNodeProvider_PreviewnetProfile(t *testing.T) {
	p := &consensusNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeConsensus, Profile: models.ProfilePreviewnet}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 48 {
		t.Errorf("expected 48 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 256 {
		t.Errorf("expected 256 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 8000 {
		t.Errorf("expected 8000 GB storage, got %d", reqs.MinStorageGB)
	}
}

func TestConsensusNodeProvider_MainnetProfile(t *testing.T) {
	p := &consensusNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeConsensus, Profile: models.ProfileMainnet}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores != 48 {
		t.Errorf("expected 48 CPU cores, got %d", reqs.MinCpuCores)
	}
	if reqs.MinMemoryGB != 256 {
		t.Errorf("expected 256 GB memory, got %d", reqs.MinMemoryGB)
	}
	if reqs.MinStorageGB != 8000 {
		t.Errorf("expected 8000 GB storage, got %d", reqs.MinStorageGB)
	}
}

func TestConsensusNodeProvider_SupportedOS(t *testing.T) {
	p := &consensusNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeConsensus, Profile: models.ProfileLocal}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs.MinSupportedOS) == 0 {
		t.Error("expected at least one supported OS")
	}
}

func TestConsensusNodeProvider_ComputeWithWhy(t *testing.T) {
	p := &consensusNodeProvider{}
	spec := DeploymentSpec{NodeType: models.NodeTypeConsensus, Profile: models.ProfileMainnet}
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

func TestConsensusNodeProvider_RegistryEntry(t *testing.T) {
	providers := Providers()
	p, ok := providers[models.NodeTypeConsensus]
	if !ok {
		t.Fatal("consensus provider not registered in global registry")
	}
	spec := DeploymentSpec{NodeType: models.NodeTypeConsensus, Profile: models.ProfileTestnet}
	reqs, err := p.Compute(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs.MinCpuCores <= 0 {
		t.Error("expected positive CPU cores from registry provider")
	}
}
